// Copyright 2022 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package memtier

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RoutineStatActionsConfig holds the configuration for the StatActions routine.
type RoutineStatActionsConfig struct {
	// IntervalMs is the length of the period in milliseconds in
	// which StatActions routine checks process_statactions call
	// statistics.
	IntervalMs int
	// IntervalCommand is the command to be executed in specified
	// intervals.
	IntervalCommand []string
	// IntervalCommandRunner executes the IntervalCommand.
	// "exec" forks and executes the command in a child process.
	// "memtier" runs memtier command.
	// "memtier-prompt" runs a single-string memtier-prompt
	// command that is allowed to contain pipe to shell.
	IntervalCommandRunner string
	// PageOutMB is the total size of memory in megabytes that
	// has been process_statactions. If advised memory exceeds the
	// interval, the shell command will be executed.
	PageOutMB int
	// PageOutCommand is executed when new PageOutMB is reached.
	PageOutCommand []string
	// PageOutCommandRunner executes the PageOutCommand.
	// See IntervalCommandRunner for options.
	PageOutCommandRunner string
	// Timestamp defines the format of a timestamp that is printed
	// before running a command. The default is empty: no
	// timestamp. Use lowercase definitions in golang Time.Format
	// (for instance "rfc3339nano" or "rfc822z") or use "unix",
	// "unix.milli", "unix.micro".
	Timestamp string
	// TimestampAfter is like timestamp but printed after
	// running a command.
	TimestampAfter string
}

// RoutineStatActions represents the StatActions routine.
type RoutineStatActions struct {
	config           *RoutineStatActionsConfig
	lastPageOutPages uint64
	policy           Policy
	cgLoop           chan interface{}
	startedUnixFloat float64
}

type commandRunnerFunc func([]string) error

const (
	commandRunnerDefault       = ""
	commandRunnerExec          = "exec"
	commandRunnerMemtier       = "memtier"
	commandRunnerMemtierPrompt = "memtier-prompt"
)

var commandRunners map[string]commandRunnerFunc

func init() {
	RoutineRegister("statactions", NewRoutineStatActions)
}

// NewRoutineStatActions creates a new instance of RoutineStatActions.
func NewRoutineStatActions() (Routine, error) {
	r := &RoutineStatActions{}
	commandRunners = map[string]commandRunnerFunc{
		commandRunnerExec:          r.runCommandExec,
		commandRunnerDefault:       r.runCommandExec,
		commandRunnerMemtier:       r.runCommandMemtier,
		commandRunnerMemtierPrompt: r.runCommandMemtierPrompt,
	}
	return r, nil
}

// SetConfigJSON sets the configuration for the StatActions routine from a JSON string.
func (r *RoutineStatActions) SetConfigJSON(configJSON string) error {
	config := &RoutineStatActionsConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	if config.IntervalMs <= 0 {
		return fmt.Errorf("invalid stataction routine configuration, IntervalMs must be > 0")
	}
	if len(config.IntervalCommand) == 0 &&
		len(config.PageOutCommand) == 0 {
		return fmt.Errorf("invalid stataction routine configuration, no actions specified (command missing)")
	}
	r.config = config
	return nil
}

// GetConfigJSON returns the configuration of the StatActions routine as a JSON string.
func (r *RoutineStatActions) GetConfigJSON() string {
	if r.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(r.config); err == nil {
		return string(configStr)
	}
	return ""
}

// SetPolicy sets the policy for the StatActions routine.
func (r *RoutineStatActions) SetPolicy(policy Policy) error {
	r.policy = policy
	return nil
}

// Start starts the StatActions routine.
func (r *RoutineStatActions) Start() error {
	if r.config == nil {
		return fmt.Errorf("cannot start without configuration")
	}
	if r.cgLoop != nil {
		return fmt.Errorf("already started")
	}
	r.cgLoop = make(chan interface{})
	r.startedUnixFloat = float64(time.Now().UnixNano()) / 1e9
	go r.loop()
	return nil
}

// Stop stops the StatActions routine.
func (r *RoutineStatActions) Stop() {
	if r.cgLoop != nil {
		log.Debugf("Stopping statactions routine")
		r.cgLoop <- struct{}{}
	} else {
		log.Debugf("statactions routine already stopped")
	}
}

// Dump returns a string representation of the StatActions routine's status.
func (r *RoutineStatActions) Dump(args []string) string {
	return fmt.Sprintf("routine \"statactions\": running=%v", r.cgLoop != nil)
}

func (r *RoutineStatActions) runCommandExec(command []string) error {
	if len(command) == 0 {
		return nil
	}
	cmd := exec.Command(command[0], command[1:]...)
	err := cmd.Run()
	stats.Store(StatsHeartbeat{fmt.Sprintf("RoutineStatActions.command.exec: %s... status: %s error: %s", command[0], cmd.ProcessState, err)})
	return err
}

func (r *RoutineStatActions) runCommandMemtier(command []string) error {
	if len(command) == 0 {
		return nil
	}
	prompt := NewPrompt("", nil, bufio.NewWriter(os.Stdout))
	if r.policy != nil {
		prompt.SetPolicy(r.policy)
	}
	status := prompt.RunCmdSlice(command)
	stats.Store(StatsHeartbeat{fmt.Sprintf("RoutineStatActions.command.memtier: %s... status: %d", command[0], status)})
	return nil
}

func (r *RoutineStatActions) runCommandMemtierPrompt(command []string) error {
	if len(command) == 0 {
		return nil
	}
	if len(command) > 1 {
		return fmt.Errorf("invalid command for command runner %q: expected a list with a single string, got %q", commandRunnerMemtierPrompt, command)
	}
	prompt := NewPrompt("", nil, bufio.NewWriter(os.Stdout))
	if r.policy != nil {
		prompt.SetPolicy(r.policy)
	}
	status := prompt.RunCmdString(command[0])
	stats.Store(StatsHeartbeat{fmt.Sprintf("RoutineStatActions.command.memtier-prompt: %q status: %d", command[0], status)})
	return nil
}

func (r *RoutineStatActions) timestamp(tsFmt string) string {
	t := time.Now()
	fmts := map[string]string{
		"ansic":       time.ANSIC,
		"unixdate":    time.UnixDate,
		"rfc822":      time.RFC822,
		"rfc822z":     time.RFC822Z,
		"rfc850":      time.RFC850,
		"rfc1123":     time.RFC1123,
		"rfc3339":     time.RFC3339,
		"rfc3339nano": time.RFC3339Nano,
	}
	keys := make([]string, 0, len(fmts))
	for key := range fmts {
		keys = append(keys, key)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	for _, key := range keys {
		tsFmt = strings.Replace(tsFmt, key, fmts[key], -1)
	}
	s := t.Format(tsFmt)
	unixTimeFloat := float64(t.UnixNano()) / 1e9
	for strings.Contains(s, "unix") {
		token := "unix"
		d := 0
		switch {
		case strings.Contains(s, "unix.s"):
			token = "unix.s"
		case strings.Contains(s, "unix.milli"):
			d = 3
			token = "unix.milli"
		case strings.Contains(s, "unix.micro"):
			d = 6
			token = "unix.micro"
		case strings.Contains(s, "unix.nano"):
			d = 9
			token = "unix.nano"
		}
		unixTimeStr := fmt.Sprintf("%."+strconv.Itoa(d)+"f",
			unixTimeFloat)
		s = strings.Replace(s, token, unixTimeStr, -1)
	}
	for strings.Contains(s, "duration") {
		token := "duration"
		d := 0
		switch {
		case strings.Contains(s, "duration.s"):
			token = "duration.s"
		case strings.Contains(s, "duration.milli"):
			d = 3
			token = "duration.milli"
		case strings.Contains(s, "duration.micro"):
			d = 6
			token = "duration.micro"
		case strings.Contains(s, "duration.nano"):
			d = 9
			token = "duration.nano"
		}
		durationTimeStr := fmt.Sprintf("%."+strconv.Itoa(d)+"f",
			unixTimeFloat-r.startedUnixFloat)
		s = strings.Replace(s, token, durationTimeStr, -1)
	}
	return s
}

func (r *RoutineStatActions) runCommand(runner string, command []string) error {
	commandRunner, ok := commandRunners[runner]
	if !ok {
		return fmt.Errorf("invalid command runner '%s'", runner)
	}
	if r.config.Timestamp != "" {
		fmt.Print(r.timestamp(r.config.Timestamp))
	}
	err := commandRunner(command)
	if r.config.TimestampAfter != "" {
		fmt.Print(r.timestamp(r.config.TimestampAfter))
	}
	return err
}

func (r *RoutineStatActions) loop() {
	log.Debugf("RoutineStatActions: online\n")
	defer log.Debugf("RoutineStatActions: offline\n")
	ticker := time.NewTicker(time.Duration(r.config.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	// Get initial values of stats that trigger actions
	r.lastPageOutPages = stats.MadvisedPageCount(-1, -1)
	quit := false
	for {
		// Wait for the next tick or Stop()
		select {
		case <-r.cgLoop:
			quit = true
		case <-ticker.C:
			stats.Store(StatsHeartbeat{"RoutineStatActions.tick"})
		}
		if quit {
			break
		}
		nowPageOutPages := stats.MadvisedPageCount(-1, -1)

		// IntervalCommand runs on every round
		if len(r.config.IntervalCommand) > 0 {
			if err := r.runCommand(r.config.IntervalCommandRunner, r.config.IntervalCommand); err != nil {
				log.Errorf("routines statactions intervalcommand: %s", err)
			}
			stats.Store(StatsHeartbeat{"RoutineStatActions.IntervalCommand"})
		}

		// PageOutCommand runs if PageOutMB is reached
		if len(r.config.PageOutCommand) > 0 &&
			(nowPageOutPages-r.lastPageOutPages)*constUPagesize/uint64(1024*1024) >= uint64(r.config.PageOutMB) {
			r.lastPageOutPages = nowPageOutPages
			stats.Store(StatsHeartbeat{"RoutineStatActions.PageOutCommand"})
			if err := r.runCommand(r.config.PageOutCommandRunner, r.config.PageOutCommand); err != nil {
				log.Errorf("routines statactions pageoutcommand: %s", err)
			}
		}
	}
	close(r.cgLoop)
	r.cgLoop = nil
}

// Copyright 2021 Intel Corporation. All Rights Reserved.
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

package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/intel/memtierd/pkg/memtier"
	_ "github.com/intel/memtierd/pkg/version"
)

type config struct {
	Policy   memtier.PolicyConfig
	Routines []memtier.RoutineConfig
}

func exit(format string, a ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, "memtierd: "+format+"\n", a...)
	os.Exit(1)
}

func loadConfigFile(filename string) (memtier.Policy, []memtier.Routine) {
	configBytes, err := os.ReadFile(filename)
	if err != nil {
		exit("%s", err)
	}
	var config config
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		exit("error in %q: %s", filename, err)
	}

	policy, err := memtier.NewPolicy(config.Policy.Name)
	if err != nil {
		exit("%s", err)
	}

	err = policy.SetConfigJSON(config.Policy.Config)
	if err != nil {
		exit("%s", err)
	}

	routines := []memtier.Routine{}
	for _, routineCfg := range config.Routines {
		routine, err := memtier.NewRoutine(routineCfg.Name)
		if err != nil {
			exit("%s", err)
		}
		err = routine.SetConfigJSON(routineCfg.Config)
		if err != nil {
			exit("routine %s: %s", routineCfg.Name, err)
		}
		routines = append(routines, routine)
	}
	return policy, routines
}

func main() {
	optPrompt := flag.Bool("prompt", false, "Run commands from standard input (after commands from -c and -f)")
	optConfig := flag.String("config", "", "Load policy and routines from a config FILE")
	optDebug := flag.Bool("debug", false, "Print debug output")
	optCommandString := flag.String("c", "", "Run commands from STRING")
	optCommandFile := flag.String("f", "", "Run commands from FILE")
	optLog := flag.String("l", "", "Write log to FILE, supports \"stdout\" and \"stderr\"")
	optEcho := flag.Bool("echo", false, "Echo commands before executing, affects -c, -f, and -prompt")

	flag.Parse()

	switch *optLog {
	case "", "stderr":
		memtier.SetLogger(log.New(os.Stderr, "", 0))
	case "-", "stdout":
		memtier.SetLogger(log.New(os.Stdout, "", 0))
	default:
		logFile, err := os.OpenFile(*optLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			exit("failed to open log file %q: %v", *optLog, err)
		}
		memtier.SetLogger(log.New(logFile, "", 0))
	}
	memtier.SetLogDebug(*optDebug)

	// Create policy/routines and run commands in the following order:
	// 1. parse config file, start policy and routines if present
	// 2. run string commands from command line
	// 3. run command file from command file
	// 4. run commands from standard input (interactive mode)
	// Quitting interactive prompt exits memtierd immediately.
	// Otherwise (no interactive prompt or it is not quitting) if policy
	// or routines are configured, memtierd will not exit.

	if *optConfig == "" && *optCommandString == "" && *optCommandFile == "" && !*optPrompt {
		exit("required at least one of: -config CONFIGFILE, -c COMMANDS, -f COMMANDFILE, or -prompt")
	}

	var prompt *memtier.Prompt
	if *optPrompt || *optCommandFile != "" || *optCommandString != "" {
		prompt = memtier.NewPrompt("memtierd> ", bufio.NewReader(os.Stdin), bufio.NewWriter(os.Stdout))
		prompt.SetEcho(*optEcho)
	}

	var policy memtier.Policy
	var routines []memtier.Routine
	if *optConfig != "" {
		policy, routines = loadConfigFile(*optConfig)
	}

	if policy != nil {
		if err := policy.Start(); err != nil {
			exit("error in starting policy: %s", err)
		}
		if prompt != nil {
			prompt.SetPolicy(policy)
		}
	}

	for r, routine := range routines {
		if policy != nil {
			if err := routine.SetPolicy(policy); err != nil {
				exit("error in setting policy for routine: %s", err)
			}
		}
		if err := routine.Start(); err != nil {
			exit("error in starting routine %d: %s", r+1, err)
		}
	}
	if prompt != nil {
		prompt.SetRoutines(routines)
	}

	if *optCommandString != "" {
		prompt.SetInput(bufio.NewReader(strings.NewReader(*optCommandString)))
		memtier.Log().Debugf("executing commands from command line")
		prompt.Interact()
	}

	if *optCommandFile != "" {
		commandFile, err := os.Open(*optCommandFile)
		if err != nil {
			exit("error in opening command file %q: %v", *optCommandFile, err)
		}
		prompt.SetInput(bufio.NewReader(commandFile))
		memtier.Log().Debugf("executing commands from file %q", *optCommandFile)
		prompt.Interact()
		commandFile.Close()
	}

	if *optPrompt {
		prompt.SetInput(bufio.NewReader(os.Stdin))
		memtier.Log().Debugf("executing commands from standard input")
		prompt.Interact()
	} else if policy != nil || len(routines) > 0 {
		memtier.Log().Debugf("running the policy and routines")
		select {}
	}
}

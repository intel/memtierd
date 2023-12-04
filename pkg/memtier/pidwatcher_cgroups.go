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

package memtier

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PidWatcherCgroupsConfig holds the configuration for the Cgroups-based PID watcher.
type PidWatcherCgroupsConfig struct {
	IntervalMs int
	Cgroups    []string // list of absolute cgroup directory paths
}

// PidWatcherCgroups is a type implementing the PidWatcher interface for Cgroups-based PID watching.
type PidWatcherCgroups struct {
	config       *PidWatcherCgroupsConfig
	pidsReported map[int]setMemberType
	pidListener  PidListener
	stop         bool
	mutex        sync.Mutex
}

func init() {
	PidWatcherRegister("cgroups", NewPidWatcherCgroups)
}

// NewPidWatcherCgroups creates a new instance of the Cgroups-based PID watcher.
func NewPidWatcherCgroups() (PidWatcher, error) {
	w := &PidWatcherCgroups{
		pidsReported: map[int]setMemberType{},
	}
	return w, nil
}

// SetConfigJSON sets the configuration for the Cgroups-based PID watcher.
func (w *PidWatcherCgroups) SetConfigJSON(configJSON string) error {
	config := &PidWatcherCgroupsConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	if config.IntervalMs == 0 {
		config.IntervalMs = 5000
	}
	if len(config.Cgroups) == 0 {
		log.Warnf("PidWatcherCgroups: cgroups config is missing\n")
	}
	w.config = config
	return nil
}

// GetConfigJSON gets the configuration for the Cgroups-based PID watcher in JSON format.
func (w *PidWatcherCgroups) GetConfigJSON() string {
	if w.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(w.config); err == nil {
		return string(configStr)
	}
	return ""
}

// SetPidListener sets the listener for PID changes.
func (w *PidWatcherCgroups) SetPidListener(l PidListener) {
	w.pidListener = l
}

// Poll initiates a single polling operation for PID changes.
func (w *PidWatcherCgroups) Poll() error {
	w.stop = false
	w.loop(true)
	return nil
}

// Start starts the PID watcher with periodic polling.
func (w *PidWatcherCgroups) Start() error {
	w.stop = false
	go w.loop(false)
	return nil
}

// Stop stops the PID watcher.
func (w *PidWatcherCgroups) Stop() {
	w.stop = true
}

func (w *PidWatcherCgroups) loop(singleshot bool) {
	log.Debugf("PidWatcherCgroups: online\n")
	defer log.Debugf("PidWatcherCgroups: offline\n")
	if w.config == nil {
		log.Errorf("PidWatcherCgroups: cannot start loop without configuration")
		return
	}
	ticker := time.NewTicker(time.Duration(w.config.IntervalMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		stats.Store(StatsHeartbeat{"PidWatcherCgroups.loop"})
		// Look for all pid files in the current cgroup hierarchy.
		procPaths := map[string]setMemberType{}
		for _, cgroupPath := range w.config.Cgroups {
			for _, path := range findFiles(cgroupPath, "cgroup.procs") {
				procPaths[path] = setMember
			}
		}

		// Read all pids in pid files.
		pidsFound := map[int]setMemberType{}
		for path := range procPaths {
			pidsNow, err := readPids(path)
			if err != nil {
				delete(procPaths, path)
			}
			for _, pid := range pidsNow {
				pidsFound[pid] = setMember
			}
		}

		// If requested to stop, quit without informing listeners.
		if w.stop {
			break
		}

		w.mutex.Lock()

		// Gather found pids that have not been reported.
		newPids := []int{}
		for foundPid := range pidsFound {
			if _, ok := w.pidsReported[foundPid]; !ok {
				w.pidsReported[foundPid] = setMember
				newPids = append(newPids, foundPid)
			}
		}

		// Gather reported pids that have disappeared.
		oldPids := []int{}
		for oldPid := range w.pidsReported {
			if _, ok := pidsFound[oldPid]; !ok {
				delete(w.pidsReported, oldPid)
				oldPids = append(oldPids, oldPid)
			}
		}

		w.mutex.Unlock()

		// Report if there are any changes in pids.
		if len(newPids) > 0 {
			if w.pidListener != nil {
				w.pidListener.AddPids(newPids)
			} else {
				log.Warnf("pidwatcher cgroup: ignoring new pids %v because nobody is listening", newPids)
			}
		}
		if len(oldPids) > 0 {
			if w.pidListener != nil {
				w.pidListener.RemovePids(oldPids)
			} else {
				log.Warnf("pidwatcher cgroup: ignoring disappeared pids %v because nobody is listening", oldPids)
			}
		}

		// If only one execution was requested, quit without waiting.
		if singleshot {
			break
		}

		// Wait for next tick.
		select {
		case <-ticker.C:
			continue
		}
	}
}

func readPids(path string) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(content), "\n")
	pids := make([]int, 0, len(lines))
	for index, line := range lines {
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("bad pid at %s:%d (%q): %s",
				path, index+1, line, err)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func findFiles(root string, filename string) []string {
	matchingFiles := []string{}
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == filename {
			matchingFiles = append(matchingFiles, path)
		}
		return nil
	})
	return matchingFiles
}

// Dump generates a string representation of the Cgroups-based PID watcher for debugging purposes.
func (w *PidWatcherCgroups) Dump([]string) string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return fmt.Sprintf("PidWatcherCgroups{config:%v,pidsReported:%v,pidListener:%v,stop:%v}",
		w.config, w.pidsReported, w.pidListener, w.stop)
}

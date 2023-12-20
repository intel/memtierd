// Copyright 2023 Intel Corporation. All Rights Reserved.
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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PidWatcherProcConfig holds the configuration for PidWatcherProc.
type PidWatcherProcConfig struct {
	IntervalMs int // poll interval
}

// PidWatcherProc is an implementation of PidWatcher that watches for new processes under /proc.
type PidWatcherProc struct {
	config       *PidWatcherProcConfig
	pidsReported map[int]setMemberType
	pidListener  PidListener
	stop         bool
	mutex        sync.Mutex
}

func init() {
	PidWatcherRegister("proc", NewPidWatcherProc)
}

// NewPidWatcherProc creates a new instance of PidWatcherProc with default configuration.
func NewPidWatcherProc() (PidWatcher, error) {
	w := &PidWatcherProc{
		pidsReported: map[int]setMemberType{},
	}
	// This pidwatcher is expected to work out-of-the-box without
	// any configuration. Set the defaults immediately.
	_ = w.SetConfigJSON("")
	return w, nil
}

// SetConfigJSON is a method of PidWatcherProc that sets the configuration from JSON.
// It unmarshals the input JSON string into the PidWatcherProcConfig structure.
func (w *PidWatcherProc) SetConfigJSON(configJSON string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	config := &PidWatcherProcConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	if config.IntervalMs == 0 {
		config.IntervalMs = 5000
	}
	w.config = config
	return nil
}

// GetConfigJSON is a method of PidWatcherProc that returns the current configuration as a JSON string.
func (w *PidWatcherProc) GetConfigJSON() string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(w.config); err == nil {
		return string(configStr)
	}
	return ""
}

// SetPidListener is a method of PidWatcherProc that sets the PidListener.
func (w *PidWatcherProc) SetPidListener(l PidListener) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.pidListener = l
}

// Poll is a method of PidWatcherProc that triggers the polling process.
// It locks the mutex, starts the polling loop, and unlocks the mutex.
func (w *PidWatcherProc) Poll() error {
	w.mutex.Lock()
	w.stop = false
	w.mutex.Unlock()
	w.loop(true)
	return nil
}

// Start is a method of PidWatcherProc that starts the polling process in a goroutine.
// It locks the mutex, starts the polling loop in a separate goroutine, and unlocks the mutex.
func (w *PidWatcherProc) Start() error {
	w.mutex.Lock()
	w.stop = false
	w.mutex.Unlock()
	go w.loop(false)
	return nil
}

// Stop is a method of PidWatcherProc that stops the polling process by setting the stop flag.
func (w *PidWatcherProc) Stop() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.stop = true
}

// Dump is a method of PidWatcherProc that returns a string representation of the current instance.
func (w *PidWatcherProc) Dump([]string) string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return fmt.Sprintf("PidWatcherProc{config:%v,pidsReported:%v,pidListener:%v,stop:%v",
		w.config, w.pidsReported, w.pidListener, w.stop)
}

func (w *PidWatcherProc) readPids() map[int]setMemberType {
	pidsFound := map[int]setMemberType{}
	matches, err := filepath.Glob("/proc/*/exe")
	if err != nil {
		stats.Store(StatsHeartbeat{fmt.Sprintf("PidWatcherProc.error: glob failed: %s", err)})
	}
	for _, file := range matches {
		if _, err := os.Readlink(file); err != nil {
			// a kernel thread or the process is already gone
			continue
		}
		if pid, err := strconv.Atoi(strings.Split(file, "/")[2]); err == nil {
			pidsFound[pid] = setMember
		}
	}
	return pidsFound
}

func (w *PidWatcherProc) calculateNewAndOldPids(pidsFound map[int]setMemberType) ([]int, []int) {
	newPids := []int{}
	oldPids := []int{}

	for foundPid := range pidsFound {
		if _, ok := w.pidsReported[foundPid]; !ok {
			w.pidsReported[foundPid] = setMember
			newPids = append(newPids, foundPid)
		}
	}

	for oldPid := range w.pidsReported {
		if _, ok := pidsFound[oldPid]; !ok {
			delete(w.pidsReported, oldPid)
			oldPids = append(oldPids, oldPid)
		}
	}

	return newPids, oldPids
}

func (w *PidWatcherProc) reportNewAndOldPids(newPids, oldPids []int) {
	if len(newPids) > 0 {
		if w.pidListener != nil {
			w.pidListener.AddPids(newPids)
		} else {
			log.Warnf("pidwatcher proc: ignoring new pids %v because nobody is listening", newPids)
		}
	}

	if len(oldPids) > 0 {
		if w.pidListener != nil {
			w.pidListener.RemovePids(oldPids)
		} else {
			log.Warnf("pidwatcher proc: ignoring disappeared pids %v because nobody is listening", oldPids)
		}
	}
}

// loop is a helper function of PidWatcherProc that performs the actual polling.
// It continuously looks for new processes under /proc and reports changes to the PidListener.
func (w *PidWatcherProc) loop(singleshot bool) {
	log.Debugf("PidWatcherProc: online\n")
	defer log.Debugf("PidWatcherProc: offline\n")
	ticker := time.NewTicker(time.Duration(w.config.IntervalMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		stats.Store(StatsHeartbeat{"PidWatcherProc.loop"})
		// Read all pids under /proc
		pidsFound := w.readPids()

		w.mutex.Lock()

		// If requested to stop, quit without informing listeners.
		if w.stop {
			w.mutex.Unlock()
			break
		}

		// Gather found pids that have not been reported and reported pids that have disappeared.
		newPids, oldPids := w.calculateNewAndOldPids(pidsFound)

		// Report if there are any changes in pids.
		w.reportNewAndOldPids(newPids, oldPids)

		w.mutex.Unlock()

		// If only one execution was requested, quit without waiting.
		if singleshot {
			break
		}

		// Wait for next tick.
		//nolint:gosimple //allow `select` with a single case
		select {
		case <-ticker.C:
			continue
		}
	}
}

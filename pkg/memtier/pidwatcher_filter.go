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
	"path/filepath"
	"regexp"
	"sync"
)

// PidWatcherFilterConfig holds the configuration for PidWatcherFilter.
type PidWatcherFilterConfig struct {
	Source  PidWatcherConfig
	Filters []*PidFilterConfig
}

// PidFilterConfig holds the configuration for a PidWatcherFilter filter.
type PidFilterConfig struct {
	Exclude               bool
	ProcExeRegexp         string
	compiledProcExeRegexp *regexp.Regexp
}

// PidWatcherFilter is an implementation of PidWatcher that filters PIDs based on configured rules.
type PidWatcherFilter struct {
	config      *PidWatcherFilterConfig
	source      PidWatcher
	pidListener PidListener
	mutex       sync.Mutex
}

// FilteringPidListener is a listener for PidWatcherFilter that filters and forwards PID changes.
type FilteringPidListener struct {
	w         *PidWatcherFilter
	addedPids map[int]setMemberType
}

func init() {
	PidWatcherRegister("filter", NewPidWatcherFilter)
}

// NewPidWatcherFilter creates a new instance of PidWatcherFilter.
func NewPidWatcherFilter() (PidWatcher, error) {
	return &PidWatcherFilter{}, nil
}

// SetConfigJSON is a method of PidWatcherFilter that sets the configuration from JSON.
// It creates a new source PidWatcher based on the provided configuration.
func (w *PidWatcherFilter) SetConfigJSON(configJSON string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	config := &PidWatcherFilterConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	newSource, err := NewPidWatcher(config.Source.Name)
	if err != nil {
		return fmt.Errorf("pidwatcher filter failed to create source: %w", err)
	}
	if err = newSource.SetConfigJSON(config.Source.Config); err != nil {
		return fmt.Errorf("configuring pidwatcher filter's source pidwatcher %q failed: %w", config.Source.Name, err)
	}
	// Validate filters.
	for _, fc := range config.Filters {
		if fc.ProcExeRegexp != "" {
			re, err := regexp.Compile(fc.ProcExeRegexp)
			if err != nil {
				return fmt.Errorf("pidwatcher filter: invalid ProcExeRegexp: %q: %w", fc.ProcExeRegexp, err)
			}
			fc.compiledProcExeRegexp = re
		}
	}
	w.source = newSource
	w.config = config
	f := &FilteringPidListener{
		addedPids: map[int]setMemberType{},
		w:         w,
	}
	if w.source != nil {
		w.source.SetPidListener(f)
	}
	return nil
}

// GetConfigJSON is a method of PidWatcherFilter that returns the current configuration as a JSON string.
func (w *PidWatcherFilter) GetConfigJSON() string {
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

// SetPidListener is a method of PidWatcherFilter that sets the PidListener.
func (w *PidWatcherFilter) SetPidListener(l PidListener) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.pidListener = l
}

// Poll is a method of PidWatcherFilter that triggers the polling process of the source PidWatcher.
func (w *PidWatcherFilter) Poll() error {
	w.mutex.Lock()
	if w.source == nil {
		w.mutex.Unlock()
		return fmt.Errorf("pidwatcher filter: poll: missing pid source")
	}
	w.mutex.Unlock()
	return w.source.Poll()
}

// Start is a method of PidWatcherFilter that starts the source PidWatcher.
func (w *PidWatcherFilter) Start() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.source == nil {
		return fmt.Errorf("pidwatcher filter: start: missing pid source")
	}
	return w.source.Start()
}

// Stop is a method of PidWatcherFilter that stops the source PidWatcher.
func (w *PidWatcherFilter) Stop() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.source == nil {
		return
	}
	w.source.Stop()
}

// Dump is a method of PidWatcherFilter that returns a string representation of the current instance.
func (w *PidWatcherFilter) Dump([]string) string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return fmt.Sprintf("%+v", w)
}

// AddPids is a method of FilteringPidListener that filters and forwards new PIDs.
func (f *FilteringPidListener) AddPids(pids []int) {
	f.w.mutex.Lock()
	defer f.w.mutex.Unlock()
	passedPids := []int{}
	for _, pid := range pids {
		for _, fc := range f.w.config.Filters {
			if fc.compiledProcExeRegexp != nil {
				exeFilepath, err := filepath.EvalSymlinks(fmt.Sprintf("/proc/%d/exe", pid))
				if err != nil {
					// pid does not exist anymore, never mind about the rest of the filters
					break
				}
				matched := fc.compiledProcExeRegexp.MatchString(exeFilepath)
				if (matched && !fc.Exclude) || (!matched && fc.Exclude) {
					passedPids = append(passedPids, pid)
				}
			}
		}
	}
	for _, pid := range passedPids {
		f.addedPids[pid] = setMember
	}
	if f.w.pidListener != nil {
		f.w.pidListener.AddPids(passedPids)
	} else {
		log.Warnf("pidwatcher filter: ignoring new pids %v because nobody is listening", passedPids)
	}
}

// RemovePids is a method of FilteringPidListener that filters and forwards disappeared PIDs.
func (f *FilteringPidListener) RemovePids(pids []int) {
	f.w.mutex.Lock()
	defer f.w.mutex.Unlock()
	passedPids := []int{}
	for _, pid := range pids {
		if _, ok := f.addedPids[pid]; ok {
			passedPids = append(passedPids, pid)
			delete(f.addedPids, pid)
		}
	}
	if f.w.pidListener != nil {
		f.w.pidListener.RemovePids(passedPids)
	} else {
		log.Warnf("pidwatcher filter: ignoring disappeared pids %v because nobody is listening", passedPids)
	}
}

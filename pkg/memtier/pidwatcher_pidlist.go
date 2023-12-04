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
)

// PidWatcherPidlistConfig holds the configuration for PidWatcherPidlist.
type PidWatcherPidlistConfig struct {
	Pids []int // list of absolute cgroup directory paths
}

// PidWatcherPidlist is an implementation of PidWatcher that watches a predefined list of PIDs.
type PidWatcherPidlist struct {
	config      *PidWatcherPidlistConfig
	pidListener PidListener
}

func init() {
	PidWatcherRegister("pidlist", NewPidWatcherPidlist)
}

// NewPidWatcherPidlist creates a new instance of PidWatcherPidlist.
func NewPidWatcherPidlist() (PidWatcher, error) {
	return &PidWatcherPidlist{}, nil
}

// SetConfigJSON is a method of PidWatcherPidlist that sets the configuration from JSON.
// It unmarshals the input JSON string into the PidWatcherPidlistConfig structure.
func (w *PidWatcherPidlist) SetConfigJSON(configJSON string) error {
	config := &PidWatcherPidlistConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	w.config = config
	return nil
}

// GetConfigJSON is a method of PidWatcherPidlist that returns the current configuration as a JSON string.
func (w *PidWatcherPidlist) GetConfigJSON() string {
	if w.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(w.config); err == nil {
		return string(configStr)
	}
	return ""
}

// SetPidListener is a method of PidWatcherPidlist that sets the PidListener.
func (w *PidWatcherPidlist) SetPidListener(l PidListener) {
	w.pidListener = l
}

// Poll is a method of PidWatcherPidlist that simulates polling for PIDs.
// It adds the configured PIDs to the PidListener, if available.
func (w *PidWatcherPidlist) Poll() error {
	if w.config == nil {
		return fmt.Errorf("pidwatcher pidlist: tried to poll without a configuration")
	}
	if w.pidListener == nil {
		log.Warnf("pidwatcher pidlist: poll skips reporting pids %v because nobody is listening", w.config.Pids)
		return nil
	}
	w.pidListener.AddPids(w.config.Pids)
	return nil
}

// Start is a method of PidWatcherPidlist that simulates starting the PidWatcher.
// It adds the configured PIDs to the PidListener, if available.
func (w *PidWatcherPidlist) Start() error {
	if w.config == nil {
		return fmt.Errorf("pidwatcher pidlist: tried to start without a configuration")
	}
	if w.pidListener == nil {
		log.Warnf("pidwatcher pidlist: skip reporting pids %v because nobody is listening", w.config.Pids)
		return nil
	}
	w.pidListener.AddPids(w.config.Pids)
	return nil
}

// Stop (does nothing here).
func (w *PidWatcherPidlist) Stop() {
}

// Dump is a method of PidWatcherPidlist that returns a string representation of the current instance.
func (w *PidWatcherPidlist) Dump([]string) string {
	return fmt.Sprintf("%+v", w)
}

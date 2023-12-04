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
	"fmt"
	"sort"
)

// PidWatcherConfig represents the configuration for a PID watcher.
type PidWatcherConfig struct {
	Name   string
	Config string
}

// PidWatcher is an interface for managing PID watchers.
type PidWatcher interface {
	SetConfigJSON(string) error // Set new configuration.
	GetConfigJSON() string      // Get current configuration.
	SetPidListener(PidListener)
	Poll() error
	Start() error
	Stop()
	Dump([]string) string
}

// PidListener is an interface for handling PID events.
type PidListener interface {
	AddPids([]int)
	RemovePids([]int)
}

// PidWatcherCreator is a function type for creating a new PID watcher instance.
type PidWatcherCreator func() (PidWatcher, error)

// pidwatchers is a map of pidwatcher name -> pidwatcher creator.
var pidwatchers map[string]PidWatcherCreator = make(map[string]PidWatcherCreator, 0)

// PidWatcherRegister registers a new PID watcher with a given name and creator function.
func PidWatcherRegister(name string, creator PidWatcherCreator) {
	pidwatchers[name] = creator
}

// PidWatcherList returns a sorted list of available PID watcher names.
func PidWatcherList() []string {
	keys := make([]string, 0, len(pidwatchers))
	for key := range pidwatchers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// NewPidWatcher creates a new PID watcher instance based on the provided name.
func NewPidWatcher(name string) (PidWatcher, error) {
	if creator, ok := pidwatchers[name]; ok {
		return creator()
	}
	return nil, fmt.Errorf("invalid pidwatcher name %q", name)
}

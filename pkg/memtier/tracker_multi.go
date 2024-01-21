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
	"encoding/json"
	"fmt"
	"strings"
)

// TrackerMultiConfig represents the configuration for multiple trackers,
// it containing a list of TrackerConfig.
type TrackerMultiConfig struct {
	Trackers []TrackerConfig
}

// TrackerMulti is a tracker that aggregates multiple trackers, each configured independently.
type TrackerMulti struct {
	config   *TrackerMultiConfig
	trackers []Tracker
}

// init registers the TrackerMulti implementation.
func init() {
	TrackerRegister("multi", NewTrackerMulti)
}

// NewTrackerMulti creates a new instance of TrackerMulti.
func NewTrackerMulti() (Tracker, error) {
	return &TrackerMulti{}, nil
}

// SetConfigJSON sets the configuration for multiple trackers from a JSON string.
func (t *TrackerMulti) SetConfigJSON(configJSON string) error {
	config := &TrackerMultiConfig{}
	if err := UnmarshalConfig(configJSON, config); err != nil {
		return err
	}
	t.trackers = make([]Tracker, len(config.Trackers))
	for tcIndex, trackerConfig := range config.Trackers {
		newTracker, err := NewTracker(trackerConfig.Name)
		if err != nil {
			return fmt.Errorf("configuring tracker multi: creating tracker index %d (%q): %w", tcIndex, trackerConfig.Name, err)
		}
		if err = newTracker.SetConfigJSON(trackerConfig.Config); err != nil {
			return fmt.Errorf("configuring tracker multi: configuring tracker index %d (%q): %w", tcIndex, trackerConfig.Name, err)
		}
		t.trackers[tcIndex] = newTracker
	}
	t.config = config
	return nil
}

// GetConfigJSON returns the JSON representation of the multiple trackers' configuration.
func (t *TrackerMulti) GetConfigJSON() string {
	if t.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(t.config); err == nil {
		return string(configStr)
	}
	return ""
}

// AddPids adds the specified process IDs (pids) to all the trackers in the TrackerMulti.
func (t *TrackerMulti) AddPids(pids []int) {
	for _, tracker := range t.trackers {
		tracker.AddPids(pids)
	}
}

// RemovePids removes the specified process IDs (pids) from all the trackers in the TrackerMulti.
func (t *TrackerMulti) RemovePids(pids []int) {
	for _, tracker := range t.trackers {
		tracker.RemovePids(pids)
	}
}

// Start starts all the trackers in the TrackerMulti.
func (t *TrackerMulti) Start() error {
	for tIndex, tracker := range t.trackers {
		if err := tracker.Start(); err != nil {
			return fmt.Errorf("starting tracker multi: starting tracker index %d: %w", tIndex, err)
		}
	}
	return nil
}

// Stop stops all the trackers in the TrackerMulti.
func (t *TrackerMulti) Stop() {
	for _, tracker := range t.trackers {
		tracker.Stop()
	}
}

// ResetCounters resets counters for all the trackers in the TrackerMulti.
func (t *TrackerMulti) ResetCounters() {
	for _, tracker := range t.trackers {
		tracker.ResetCounters()
	}
}

// GetCounters aggregates counters from all the trackers in the TrackerMulti.
func (t *TrackerMulti) GetCounters() *TrackerCounters {
	var tcs *TrackerCounters = &TrackerCounters{}
	for _, tracker := range t.trackers {
		*tcs = append(*tcs, *tracker.GetCounters()...)
	}
	return tcs.Flattened(nil, nil)
}

// Dump returns a string representation of all the trackers.
func (t *TrackerMulti) Dump(args []string) string {
	dumps := make([]string, len(t.trackers))
	for tIndex, tracker := range t.trackers {
		dumps[tIndex] = fmt.Sprintf("tracker %d:\n%s", tIndex, tracker.Dump(args))
	}
	return strings.Join(dumps, "\n")
}

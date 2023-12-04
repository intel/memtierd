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

// TrackerStub defines empty struct for the scenarios without tracker configured.
type TrackerStub struct {
}

func init() {
	TrackerRegister("stub", NewTrackerStub)
}

// NewTrackerStub creates a new instance of TrackerStub with default configuration.
func NewTrackerStub() (Tracker, error) {
	return &TrackerStub{}, nil
}

// SetConfigJSON is a method of TrackerStub, returns nil.
func (t *TrackerStub) SetConfigJSON(configJSON string) error {
	return nil
}

// GetConfigJSON is a method of TrackerStub, returns "".
func (t *TrackerStub) GetConfigJSON() string {
	return ""
}

// AddPids is a method of TrackerStub that doing nothing here.
func (t *TrackerStub) AddPids(pids []int) {
}

// RemovePids is a method of TrackerStub that doing nothing here.
func (t *TrackerStub) RemovePids(pids []int) {
}

// Start is a method of TrackerStub, returns nil.
func (t *TrackerStub) Start() error {
	return nil
}

// Stop is a method of TrackerStub that doing nothing here.
func (t *TrackerStub) Stop() {
}

// ResetCounters is a method of TrackerStub that doing nothing here.
func (t *TrackerStub) ResetCounters() {
}

// GetCounters is a method of TrackerStub, returns nil.
func (t *TrackerStub) GetCounters() *TrackerCounters {
	return nil
}

// Dump is a method of TrackerStub, returns "".
func (t *TrackerStub) Dump([]string) string {
	return ""
}

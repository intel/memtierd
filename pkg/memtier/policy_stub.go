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

// PolicyStub defines empty struct for the scenarios without policy configured.
type PolicyStub struct {
}

func init() {
	PolicyRegister("stub", NewPolicyStub)
}

// NewPolicyStub creates a new instance of PolicyStub with default configuration.
func NewPolicyStub() (Policy, error) {
	return &PolicyStub{}, nil
}

// SetConfigJSON is a method of PolicyStub, returns nil.
func (p *PolicyStub) SetConfigJSON(configJSON string) error {
	return nil
}

// GetConfigJSON is a method of PolicyStub, returns "".
func (p *PolicyStub) GetConfigJSON() string {
	return ""
}

// Start is a method of PolicyStub, returns nil.
func (p *PolicyStub) Start() error {
	return nil
}

// Stop is a method of TrackerStub that doing nothing here.
func (p *PolicyStub) Stop() {
}

// PidWatcher is a method of PolicyStub, returns nil.
// As when there is no policy, pidwatcher does not make sense.
func (p *PolicyStub) PidWatcher() PidWatcher {
	return nil
}

// Mover is a method of PolicyStub, returns nil.
// As when there is no policy, mover does not make sense.
func (p *PolicyStub) Mover() *Mover {
	return nil
}

// Tracker is a method of PolicyStub, returns nil.
// As when there is no policy, tracker does not make sense.
func (p *PolicyStub) Tracker() Tracker {
	return nil
}

// Dump is a method of PolicyStub, returns "".
func (p *PolicyStub) Dump(args []string) string {
	return ""
}

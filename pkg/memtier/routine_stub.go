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

// RoutineStub defines empty struct for the scenarios without routines configured.
type RoutineStub struct {
}

func init() {
	RoutineRegister("stub", NewRoutineStub)
}

// NewRoutineStub creates a new instance of RoutineStub with default configuration.
func NewRoutineStub() (Routine, error) {
	return &RoutineStub{}, nil
}

// SetConfigJSON is a method of RoutineStub, returns nil.
func (r *RoutineStub) SetConfigJSON(configJSON string) error {
	return nil
}

// GetConfigJSON is a method of RoutineStub, returns "".
func (r *RoutineStub) GetConfigJSON() string {
	return ""
}

// SetPolicy is a method of RoutineStub, returns "".
func (r *RoutineStub) SetPolicy(Policy) error {
	return nil
}

// Start is a method of RoutineStub, returns nil.
func (r *RoutineStub) Start() error {
	return nil
}

// Stop is a method of RoutineStub that doing nothing here.
func (r *RoutineStub) Stop() {
}

// Dump is a method of RoutineStub, returns "".
func (r *RoutineStub) Dump(args []string) string {
	return ""
}

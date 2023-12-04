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

// PolicyConfig represents the configuration for a policy.
type PolicyConfig struct {
	Name   string
	Config string
}

// Policy interface defines the methods that a memory management policy should implement.
type Policy interface {
	SetConfigJSON(string) error // Set new configuration.
	GetConfigJSON() string      // Get current configuration.
	Start() error
	Stop()
	// PidWatcher, Mover and Tracker are mostly for debugging in interactive prompt...
	PidWatcher() PidWatcher
	Mover() *Mover
	Tracker() Tracker
	Dump(args []string) string
}

// PolicyCreator is a function that creates an instance of a Policy.
type PolicyCreator func() (Policy, error)

// policies is a map of policy name -> policy creator
var policies map[string]PolicyCreator = make(map[string]PolicyCreator, 0)

// PolicyRegister registers a new policy with its creator function.
func PolicyRegister(name string, creator PolicyCreator) {
	policies[name] = creator
}

// PolicyList returns a sorted list of available policy names.
func PolicyList() []string {
	keys := make([]string, 0, len(policies))
	for key := range policies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// NewPolicy creates a new instance of a policy based on its name.
func NewPolicy(name string) (Policy, error) {
	if creator, ok := policies[name]; ok {
		return creator()
	}
	return nil, fmt.Errorf("invalid policy name %q", name)
}

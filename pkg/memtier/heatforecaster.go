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
	"fmt"
	"sort"
)

// HeatForecasterConfig represents the configuration for a heat forecaster.
type HeatForecasterConfig struct {
	Name   string
	Config string
}

// HeatForecaster is an interface that defines the methods for a heat forecaster.
type HeatForecaster interface {
	SetConfigJSON(string) error // Set new configuration.
	GetConfigJSON() string      // Get current configuration.
	Forecast(*Heats) (*Heats, error)
	Dump(args []string) string
}

// HeatForecasterCreator is a function type that creates a new instance of HeatForecaster.
type HeatForecasterCreator func() (HeatForecaster, error)

// heatforecasters is a map of heatforecaster name -> heatforecaster creator.
var heatforecasters map[string]HeatForecasterCreator = make(map[string]HeatForecasterCreator, 0)

// HeatForecasterRegister registers a new heat forecaster with its creator function.
func HeatForecasterRegister(name string, creator HeatForecasterCreator) {
	heatforecasters[name] = creator
}

// HeatForecasterList returns a sorted list of registered heat forecaster names.
func HeatForecasterList() []string {
	keys := make([]string, 0, len(heatforecasters))
	for key := range heatforecasters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// NewHeatForecaster creates a new instance of HeatForecaster based on the provided name.
// It returns an error if the specified heat forecaster name is invalid.
func NewHeatForecaster(name string) (HeatForecaster, error) {
	if creator, ok := heatforecasters[name]; ok {
		return creator()
	}
	return nil, fmt.Errorf("invalid heat forecaster name %q", name)
}

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
	"os"
	"time"
)

// HeatForecasterTraceConfig represents the configuration for the HeatForecasterTrace.
type HeatForecasterTraceConfig struct {
	File         string
	HideHeatZero bool
}

// HeatForecasterTrace is a heat forecaster that writes heat data to a trace file.
type HeatForecasterTrace struct {
	config *HeatForecasterTraceConfig
}

// init registers the HeatForecasterTrace implementation.
func init() {
	HeatForecasterRegister("trace", NewHeatForecasterTrace)
}

// NewHeatForecasterTrace creates a new instance of HeatForecasterTrace.
func NewHeatForecasterTrace() (HeatForecaster, error) {
	return &HeatForecasterTrace{}, nil
}

// SetConfigJSON sets the configuration for the HeatForecasterTrace from a JSON string.
func (hf *HeatForecasterTrace) SetConfigJSON(configJSON string) error {
	config := &HeatForecasterTraceConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	return hf.SetConfig(config)
}

// SetConfig sets the configuration for the HeatForecasterTrace.
func (hf *HeatForecasterTrace) SetConfig(config *HeatForecasterTraceConfig) error {
	if config.File == "" {
		return fmt.Errorf("invalid trace heatforecaster configuration: 'file' missing")
	}
	hf.config = config
	return nil
}

// GetConfigJSON returns the JSON representation of the HeatForecasterTrace's configuration.
func (hf *HeatForecasterTrace) GetConfigJSON() string {
	configString, err := json.Marshal(hf.config)
	if err != nil {
		return ""
	}
	return string(configString)
}

// Forecast writes heat data to a trace file based on the configured settings.
func (hf *HeatForecasterTrace) Forecast(heats *Heats) (*Heats, error) {
	if heats == nil {
		return nil, nil
	}
	f, err := os.OpenFile(hf.config.File, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	_, _ = f.WriteString(fmt.Sprintf("table: heat forecast %d pid addr size heat created updated\n", time.Now().UnixNano()))
	for pid, heatranges := range *heats {
		for _, hr := range *heatranges {
			if hf.config.HideHeatZero && hr.heat < 0.000000001 {
				continue
			}
			_, _ = f.WriteString(fmt.Sprintf("%d %x %d %.9f %d %d\n",
				pid, hr.addr, hr.length*constUPagesize, hr.heat, hr.created, hr.updated))
		}
	}
	return nil, nil
}

// Dump returns a string representation of the HeatForecasterTrace for debugging purposes.
func (hf *HeatForecasterTrace) Dump(args []string) string {
	return "HeatForecasterTrace{config=" + hf.GetConfigJSON() + "}"
}

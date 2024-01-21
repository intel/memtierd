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
)

// HeatForecasterChainConfig represents the configuration for the HeatForecasterChain.
type HeatForecasterChainConfig struct {
	Forecasters []HeatForecasterConfig
}

// HeatForecasterChain represents a chain of heat forecasters.
type HeatForecasterChain struct {
	config      *HeatForecasterChainConfig
	forecasters []HeatForecaster
}

// init registers the HeatForecasterChain implementation.
func init() {
	HeatForecasterRegister("chain", NewHeatForecasterChain)
}

// NewHeatForecasterChain creates a new instance of HeatForecasterChain.
func NewHeatForecasterChain() (HeatForecaster, error) {
	return &HeatForecasterChain{}, nil
}

// SetConfigJSON sets the configuration for the HeatForecasterChain from a JSON string.
func (hf *HeatForecasterChain) SetConfigJSON(configJSON string) error {
	config := &HeatForecasterChainConfig{}
	if err := UnmarshalConfig(configJSON, config); err != nil {
		return err
	}
	return hf.SetConfig(config)
}

// SetConfig sets the configuration for the HeatForecasterChain.
func (hf *HeatForecasterChain) SetConfig(config *HeatForecasterChainConfig) error {
	hf.forecasters = []HeatForecaster{}
	for _, conf := range config.Forecasters {
		fc, err := NewHeatForecaster(conf.Name)
		if err != nil {
			return err
		}
		err = fc.SetConfigJSON(conf.Config)
		if err != nil {
			return err
		}
		hf.forecasters = append(hf.forecasters, fc)
	}
	hf.config = config
	return nil
}

// GetConfigJSON returns the JSON representation of the HeatForecasterChain's configuration.
func (hf *HeatForecasterChain) GetConfigJSON() string {
	configString, err := json.Marshal(hf.config)
	if err != nil {
		return ""
	}
	return string(configString)
}

// Forecast performs heat forecasting using the chain of forecasters.
func (hf *HeatForecasterChain) Forecast(heats *Heats) (*Heats, error) {
	lastNonNilHeats := heats
	for _, fc := range hf.forecasters {
		forecastedHeats, _ := fc.Forecast(lastNonNilHeats)
		if forecastedHeats != nil {
			lastNonNilHeats = forecastedHeats
		}
	}
	return lastNonNilHeats, nil
}

// Dump returns a string representation of the HeatForecasterChain for debugging purposes.
func (hf *HeatForecasterChain) Dump(args []string) string {
	return "HeatForecasterChain{config=" + hf.GetConfigJSON() + "}"
}

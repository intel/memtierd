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
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// HeatForecasterStdioConfig represents the configuration for HeatForecasterStdio,
// containing the command to execute, stderr configuration, and retry count.
type HeatForecasterStdioConfig struct {
	Command []string
	Stderr  string
	Retry   int
}

// HeatForecasterStdio is a heat forecaster that communicates with an external process through standard I/O.
type HeatForecasterStdio struct {
	config  *HeatForecasterStdioConfig
	process *exec.Cmd
	stdin   *bufio.Writer
	stdout  *bufio.Reader
	stderr  *os.File
	jsonout *json.Decoder
}

// init registers the HeatForecasterStdio implementation.
func init() {
	HeatForecasterRegister("stdio", NewHeatForecasterStdio)
}

// NewHeatForecasterStdio creates a new instance of HeatForecasterStdio.
func NewHeatForecasterStdio() (HeatForecaster, error) {
	return &HeatForecasterStdio{}, nil
}

// SetConfigJSON sets the configuration for HeatForecasterStdio from a JSON string.
func (hf *HeatForecasterStdio) SetConfigJSON(configJSON string) error {
	config := &HeatForecasterStdioConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	return hf.SetConfig(config)
}

// SetConfig sets the configuration for HeatForecasterStdio.
func (hf *HeatForecasterStdio) SetConfig(config *HeatForecasterStdioConfig) error {
	if err := hf.startProcess(config); err != nil {
		return err
	}
	hf.config = config
	return nil
}

// GetConfigJSON returns the JSON representation of the HeatForecasterStdio's configuration.
func (hf *HeatForecasterStdio) GetConfigJSON() string {
	configString, err := json.Marshal(hf.config)
	if err != nil {
		return ""
	}
	return string(configString)
}

// Forecast sends the current heats to the external process and receives forecasted heats in return.
func (hf *HeatForecasterStdio) Forecast(heats *Heats) (*Heats, error) {
	if heats == nil {
		return nil, nil
	}
	_ = hf.sendCurrentHeats(heats, hf.config.Retry, []byte{})
	log.Debugf("forecast heats for %d processes sent", len(*heats))
	newHeats := &Heats{}
	if err := hf.jsonout.Decode(&newHeats); err != nil {
		log.Errorf("forecaster stdio: failed to read new heats: %s", err)
		return nil, err
	}
	log.Debugf("forecaster stdio: heats for %d processes received", len(*newHeats))
	return newHeats, nil
}

// startProcess initializes the external process based on the provided configuration.
func (hf *HeatForecasterStdio) startProcess(config *HeatForecasterStdioConfig) error {
	if len(config.Command) == 0 {
		return fmt.Errorf("forecaster stdio: config 'Command' missing")
	}
	hf.process = exec.Command(config.Command[0], config.Command[1:]...)
	stdin, err := hf.process.StdinPipe()
	if err != nil {
		return fmt.Errorf("forecaster stdio: stdin failed: %w", err)
	}
	hf.stdin = bufio.NewWriter(stdin)
	stdout, err := hf.process.StdoutPipe()
	if err != nil {
		return fmt.Errorf("forecaster stdio: stdout failed: %w", err)
	}
	hf.stdout = bufio.NewReader(stdout)
	hf.stderr = nil
	if strings.HasPrefix(config.Stderr, "file:") {
		hf.stderr, err = os.OpenFile(config.Stderr[5:], os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("forecaster stdio: cannot open config.stderr file %q for writing: %w", config.Stderr[5:], err)
		}
	} else if config.Stderr != "" {
		return fmt.Errorf("forecaster stdio: unknown config.stderr directive %s, expecting empty or \"file:/path/to/stderr.log\"", config.Stderr)
	} else {
		// config.Stderr is undefined, copy it to stderr of memtierd.
		hf.process.Stderr = os.Stderr
	}
	err = hf.process.Start()
	if err != nil {
		return fmt.Errorf("forecaster stdio: failed to start config.Command %v: %w", config.Command, err)
	}
	hf.jsonout = json.NewDecoder(hf.stdout)
	// Call Wait() to make sure that hf.process.ProcessState will
	// be updated when process exits.
	//nolint:errcheck //ignore the err check for "go func()"
	go hf.process.Wait()
	return nil
}

// sendCurrentHeats sends the current heats to the external process with retry mechanism.
func (hf *HeatForecasterStdio) sendCurrentHeats(heats *Heats, triesLeft int, marshaled []byte) error {
	var err error
	if triesLeft < 0 {
		return fmt.Errorf("forecaster stdio: sending heats failed, out of retries")
	}
	data := marshaled
	if len(data) == 0 {
		data, err = json.Marshal(heats)
		if err != nil {
			log.Errorf("forecast stdio: failed to marshal heats into json: %w", err)
			return err
		}
	}
	if n, err := hf.stdin.Write(data); err != nil {
		log.Errorf("forecast stdio: send heat error after %d bytes: %v", n, err)
		if hf.process.ProcessState.Exited() {
			log.Errorf("forecast stdio: process exited, restarting...")
			if err := hf.startProcess(hf.config); err != nil {
				return fmt.Errorf("forecast stdio: failed to restart process: %w", err)
			}
		} else {
			data = data[n:]
			log.Errorf("forecast stdio: %d bytes not yet sent", len(data))
		}
		return hf.sendCurrentHeats(heats, triesLeft-1, data)
	}
	_, _ = hf.stdin.Write([]byte("\n"))
	hf.stdin.Flush()
	return nil
}

// Dump returns a string representation of the HeatForecasterStdio for debugging purposes.
func (hf *HeatForecasterStdio) Dump(args []string) string {
	return "HeatForecasterStdio{config=" + hf.GetConfigJSON() + "}"
}

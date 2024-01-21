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
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// convertYamlKeysToLower converts yaml's all fileds' name to lower anyway, thus,
// memtierd could be case-insensitive for the keys
func convertYamlKeysToLower(in []byte) ([]byte, error) {
	lines := []string{}
	for _, line := range strings.Split(string(in), "\n") {
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			line = fmt.Sprintf("%s:%s", strings.ToLower(parts[0]), parts[1])
		}
		lines = append(lines, line)
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// UnmarshalYamlConfig will unmarshal yaml configuration and convert keys to lower cases
func UnmarshalYamlConfig(yamlData []byte, obj interface{}) error {
	err := yaml.Unmarshal(yamlData, obj)
	if err == nil {
		newYamlData, errx := convertYamlKeysToLower(yamlData)
		if errx != nil {
			return fmt.Errorf("failed to convertYamlKeysToLower config data: %s with err: %w", yamlData, errx)
		}

		erry := yaml.Unmarshal(newYamlData, obj)
		if erry != nil {
			return fmt.Errorf("failed to yaml.Unmarshal new config data: %s with err: %w", newYamlData, erry)
		}

		return nil
	}

	return fmt.Errorf("failed to yaml.Unmarshal config data: %s with err: %w", yamlData, err)
}

// UnmarshalConfig will unmarshal yaml configuration and convert keys to lower cases if the
// configuration is yaml, or unmarshal json configuration if the configuration is json
func UnmarshalConfig(jsonOrYaml string, obj interface{}) error {
	// attempt to unmarshal yaml configuration and convert keys to lower cases
	err := UnmarshalYamlConfig([]byte(jsonOrYaml), obj)
	if err != nil {
		errx := json.Unmarshal([]byte(jsonOrYaml), obj)
		if errx != nil {
			return fmt.Errorf("failed to yaml.Unmarshal config data: %s with err: %w, json.Unmarshal config data with err: %w", jsonOrYaml, err, errx)
		}
	}
	// attempt to unmarshal json configuration
	if err := json.Unmarshal([]byte(jsonOrYaml), obj); err == nil {
		return nil
	}

	return fmt.Errorf("failed to unmarshal config data: %s", jsonOrYaml)
}

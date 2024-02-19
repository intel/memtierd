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
	"strings"

	"gopkg.in/yaml.v3"
)

func unmarshal(jsonOrYaml string, obj interface{}) error {
	decoder := json.NewDecoder(strings.NewReader(jsonOrYaml))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(obj); err != nil {
		decoder := yaml.NewDecoder(strings.NewReader(jsonOrYaml))
		decoder.KnownFields(true)
		if err := decoder.Decode(obj); err != nil {
			return err
		}
	}
	return nil
}

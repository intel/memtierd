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

// GlobalConfig represents global configurations for memtier.
type GlobalConfig struct {
	FormatTable GlobalFormatTableValue
}

// GlobalFormatTableValue represents the possible values for the format of global information display.
type GlobalFormatTableValue int

// Constants defining values for the format of global information display.
const (
	GlobalFormatTableText GlobalFormatTableValue = iota
	GlobalFormatTableCsv
)

var globalConfig *GlobalConfig

// GlobalSet sets the global configuration for the specified key with the provided value.
func GlobalSet(configKey, configValue string) error {
	return nil
}

// init initializes the package.
func init() {
	globalConfig = &GlobalConfig{
		FormatTable: GlobalFormatTableText,
	}
}

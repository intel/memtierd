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

type GlobalConfig struct {
	FormatTable GlobalFormatTableValue
}

type GlobalFormatTableValue int

const (
	GlobalFormatTableText GlobalFormatTableValue = iota
	GlobalFormatTableCsv
)

var globalConfig *GlobalConfig

func GlobalSet(configKey, configValue string) error {
	return nil
}

func init() {
	globalConfig = &GlobalConfig{
		FormatTable: GlobalFormatTableText,
	}
}

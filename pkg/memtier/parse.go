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
	"strconv"
	"strings"
)

var bytesSuffixMultiplier map[string]int64 = map[string]int64{
	"B":  1,
	"K":  1 << 10,
	"KB": 1 << 10,
	"M":  1 << 20,
	"MB": 1 << 20,
	"G":  1 << 30,
	"GB": 1 << 30,
	"T":  1 << 40,
	"TB": 1 << 40,
}

var sortedBytesSuffixes []string

// ParseBytes parses a string representation of bytes and returns the equivalent number of bytes.
// It supports units like k, M, G, T (kilo, mega, giga, tera) and expects the input string
// to be in the format of "<numeric part><unit>" (e.g., "10M" for 10 megabytes).
func ParseBytes(bytes string) (int64, error) {
	if len(bytes) == 0 {
		return 0, fmt.Errorf("empty string")
	}
	for _, suffix := range sortedBytesSuffixes {
		if strings.HasSuffix(strings.ToUpper(bytes), suffix) {
			multiplier := bytesSuffixMultiplier[suffix]
			value, err := strconv.ParseInt(
				strings.TrimSpace(strings.TrimSuffix(bytes, suffix)),
				10, 64)
			return value * multiplier, err
		}
	}
	return strconv.ParseInt(bytes, 10, 64)
}

// MustParseBytes is a helper function that wraps ParseBytes and panics if an error occurs.
// It is useful in situations where the input string is expected to be valid.
func MustParseBytes(s string) int64 {
	bytes, err := ParseBytes(s)
	if err != nil {
		panic(err)
	}
	return bytes
}

// initialize module parameters at start
func init() {
	for suffix := range bytesSuffixMultiplier {
		sortedBytesSuffixes = append(sortedBytesSuffixes, suffix)
	}
	sort.Slice(sortedBytesSuffixes, func(i, j int) bool {
		return len(sortedBytesSuffixes[i]) > len(sortedBytesSuffixes[j])
	})
}

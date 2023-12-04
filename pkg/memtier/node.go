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
)

// Node represents a numa node.
type Node int

const (
	// NodeUndefined is used when moving memory among numa nodes.
	NodeUndefined = Node(1 << 31)
	// NodeSwap is used when swapping in and out.
	NodeSwap = Node(-1)
)

// String returns a string representation of the numa Node.
func (node Node) String() string {
	switch node {
	case NodeUndefined:
		return "<undefined>"
	case NodeSwap:
		return "swap"
	default:
		return fmt.Sprintf("node%d", node)
	}
}

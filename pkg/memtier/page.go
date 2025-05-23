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

// Pages represents a collection of memory pages associated with a process.
type Pages struct {
	pid   int
	pages []Page
}

// Page represents an individual memory page with an address.
type Page struct {
	addr uint64
}

// Addr returns the address of the Page.
func (p Page) Addr() uint64 {
	return p.addr
}

// Pid returns the process ID associated with the Pages.
func (pp *Pages) Pid() int {
	return pp.pid
}

// Pages returns the slice of Page objects associated with the Pages.
func (pp *Pages) Pages() []Page {
	return pp.pages
}

// Offset returns pages starting from an offset.
func (pp *Pages) Offset(offset int) *Pages {
	pagesLeft := len(pp.pages)
	if offset > pagesLeft {
		offset = pagesLeft
	}
	if offset < 0 {
		offset = 0
	}
	return &Pages{
		pid:   pp.pid,
		pages: pp.pages[offset:],
	}
}

// InAddrRanges returns process pages that are in any of given address ranges.
func (pp *Pages) InAddrRanges(addrRanges ...AddrRange) *Pages {
	// TODO: Implement me!

	return &Pages{pid: pp.pid}
}

// AddrRanges returns up to count Pages as AddrRanges. If count is -1,
// all pages are returned.
func (pp *Pages) AddrRanges(count int) *AddrRanges {
	if count == -1 {
		count = len(pp.pages)
	}
	ar := &AddrRanges{
		pid:   pp.pid,
		addrs: []AddrRange{},
	}
	if len(pp.pages) == 0 {
		return ar
	}
	contRegionStartAddr := pp.pages[0].addr
	contRegionPageCount := uint64(1)
	totalPageCount := 1
	for _, p := range pp.pages[1:] {
		if totalPageCount >= count {
			break
		}
		if p.addr != contRegionStartAddr+contRegionPageCount*constUPagesize {
			ar.addrs = append(ar.addrs, AddrRange{
				addr:   contRegionStartAddr,
				length: contRegionPageCount,
			})
			contRegionStartAddr = p.addr
			contRegionPageCount = 1
		} else {
			contRegionPageCount++
		}
		totalPageCount++
	}
	ar.addrs = append(ar.addrs, AddrRange{
		addr:   contRegionStartAddr,
		length: contRegionPageCount,
	})
	return ar
}

// SwapOut swaps out specified number of contiguous address ranges.
func (pp *Pages) SwapOut(count int) error {
	// Build contiguous address ranges from pages and swap them out.
	ar := pp.AddrRanges(count)
	return ar.SwapOut()
}

// MoveTo moves specified number of pages to a given node.
func (pp *Pages) MoveTo(node Node, count int) (int, error) {
	pageCount, pages := pp.countAddrs()
	uCount := uint(count)
	if uCount > pageCount {
		uCount = pageCount
	}
	flags := MPOL_MF_MOVE
	pages = pages[:uCount]
	if len(pages) == 0 {
		return 0, nil
	}
	nodes := make([]int, uCount)
	intNode := int(node)
	for i := range nodes {
		nodes[i] = intNode
	}
	sysRet, status, err := movePagesSyscall(pp.pid, uCount, pages, nodes, flags)
	destNodeCount := 0
	otherNodeCount := 0
	statusErrorCounts := make(map[int]int)
	if sysRet == 0 {
		for _, node := range status {
			if node == intNode {
				destNodeCount++
			} else if node < 0 {
				statusErrorCounts[-node]++
			} else {
				otherNodeCount++
			}
		}
	} else {
		stats.Store(StatsHeartbeat{fmt.Sprintf("move_pages(...) error: %s", err)})
	}
	if stats != nil {
		stats.Store(StatsMoved{
			pid:            pp.pid,
			sysRet:         sysRet,
			destNode:       intNode,
			firstPageAddr:  pages[0],
			reqCount:       int(count),
			destNodeCount:  destNodeCount,
			otherNodeCount: otherNodeCount,
			errorCounts:    statusErrorCounts,
		})
	}
	return destNodeCount, err
}

// OnNode returns only those Pages that are on the given node.
func (pp *Pages) OnNode(node Node) *Pages {
	currentStatus, err := pp.status()
	if err != nil {
		return nil
	}
	np := &Pages{pid: pp.pid}
	intNode := int(node)
	for i, p := range pp.pages {
		if currentStatus[i] == intNode {
			np.pages = append(np.pages, p)
		}
	}
	return np
}

// OnNodes returns only those Pages that are on any of given nodes.
func (pp *Pages) OnNodes(nodeMask uint64) *Pages {
	currentStatus, err := pp.status()
	if err != nil {
		return nil
	}
	np := &Pages{pid: pp.pid}
	for i, p := range pp.pages {
		if nodeMask&(1<<currentStatus[i]) != 0 {
			np.pages = append(np.pages, p)
		}
	}
	return np
}

// NotOnNode returns only those Pages that are not on the given node.
func (pp *Pages) NotOnNode(node Node) *Pages {
	currentStatus, err := pp.status()
	if err != nil {
		return nil
	}
	np := &Pages{pid: pp.pid}
	intNode := int(node)
	for i, p := range pp.pages {
		if currentStatus[i] != intNode {
			np.pages = append(np.pages, p)
		}
	}
	return np
}

func (pp *Pages) countAddrs() (uint, []uintptr) {
	count := uint(len(pp.pages))
	addrs := make([]uintptr, count)
	for i := uint(0); i < count; i++ {
		addrs[i] = uintptr(pp.pages[i].addr)
	}
	return count, addrs
}

func (pp *Pages) status() ([]int, error) {
	pageCount, pages := pp.countAddrs()
	flags := MPOL_MF_MOVE
	_, currentStatus, err := movePagesSyscall(pp.pid, pageCount, pages, nil, flags)
	return currentStatus, err
}

// NodePageCount returns map: numanode -> number of pages on the node.
func (pp *Pages) NodePageCount() map[Node]uint {
	currentStatus, err := pp.status()
	if err != nil {
		return nil
	}

	pageErrors := 0
	nodePageCount := make(map[Node]uint)

	for _, pageStatus := range currentStatus {
		if pageStatus < 0 {
			pageErrors++
			continue
		}
		nodePageCount[Node(pageStatus)]++
	}
	return nodePageCount
}

// Nodes returns a slice of Nodes associated with the Pages.
func (pp *Pages) Nodes() []Node {
	nodePageCount := pp.NodePageCount()
	nodes := make([]Node, len(nodePageCount))
	for node := range nodePageCount {
		nodes = append(nodes, node)
	}
	return nodes
}

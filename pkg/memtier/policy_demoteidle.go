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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// pageRange is a compact representation of a contiguous page range.
// 16 bytes per range: page-aligned virtual start address + page count.
type pageRange struct {
	addr   uint64 // page-aligned virtual start address
	length uint64 // number of contiguous pages
}

// PolicyDemoteIdleConfig holds the configuration for the demote-idle policy.
type PolicyDemoteIdleConfig struct {
	PidWatcher PidWatcherConfig
	Mover      MoverConfig
	// IntervalMs is the length of the period in milliseconds
	// between idle page scan rounds.
	IntervalMs int
	// IdleNumas is the list of NUMA nodes where idle pages should
	// be moved to.
	IdleNumas []int
	// PagesOfInterestDir is an optional directory path. When set,
	// PIDs and their pages of interest are read from binary .vab
	// (vab stands for Virtual Addresses, Binary format)
	// files in this directory instead of (or in addition to) the
	// PidWatcher/procMaps approach. Each file is named <PID>.vab
	// and contains a sequence of little-endian (start, end) uint64
	// address pairs. A "lock" file in the directory is used for
	// flock(2)-based reader/writer synchronization.
	PagesOfInterestDir string
}

// PolicyDemoteIdle implements the Policy interface.
// It demotes pages that have not been accessed (kernel idle page flag
// still set) since the previous scan round.
type PolicyDemoteIdle struct {
	config     *PolicyDemoteIdleConfig
	pidwatcher PidWatcher
	mover      *Mover
	mutex      sync.Mutex
	stop       chan struct{}
	poi        map[int][]pageRange // pages of interest per pid
}

func init() {
	PolicyRegister("demote-idle", NewPolicyDemoteIdle)
}

// NewPolicyDemoteIdle creates a new instance of the demote-idle policy.
func NewPolicyDemoteIdle() (Policy, error) {
	return &PolicyDemoteIdle{
		mover: NewMover(),
		poi:   make(map[int][]pageRange),
	}, nil
}

// SetConfigJSON sets the policy configuration from a JSON string.
func (p *PolicyDemoteIdle) SetConfigJSON(configJSON string) error {
	config := &PolicyDemoteIdleConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	return p.SetConfig(config)
}

// SetConfig validates and applies the configuration.
func (p *PolicyDemoteIdle) SetConfig(config *PolicyDemoteIdleConfig) error {
	if config.IntervalMs <= 0 {
		return fmt.Errorf("invalid demote-idle policy IntervalMs: %d, > 0 expected", config.IntervalMs)
	}
	if len(config.IdleNumas) == 0 {
		return fmt.Errorf("demote-idle policy requires at least one IdleNumas entry")
	}
	if config.PidWatcher.Name == "" && config.PagesOfInterestDir == "" {
		return fmt.Errorf("demote-idle policy requires pidwatcher or pagesofinterestdir")
	}
	if config.PagesOfInterestDir != "" {
		info, err := os.Stat(config.PagesOfInterestDir)
		if err != nil {
			return fmt.Errorf("pagesofinterestdir %q: %w", config.PagesOfInterestDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("pagesofinterestdir %q is not a directory", config.PagesOfInterestDir)
		}
	}
	if config.PidWatcher.Name != "" {
		newPidWatcher, err := NewPidWatcher(config.PidWatcher.Name)
		if err != nil {
			return err
		}
		if err = newPidWatcher.SetConfigJSON(config.PidWatcher.Config); err != nil {
			return fmt.Errorf("configuring pidwatcher %q for the demote-idle policy failed: %w", config.PidWatcher.Name, err)
		}
		p.pidwatcher = newPidWatcher
	} else {
		p.pidwatcher = nil
	}
	if err := p.mover.SetConfig(&config.Mover); err != nil {
		return fmt.Errorf("configuring mover failed: %s", err)
	}
	p.config = config
	return nil
}

// GetConfigJSON returns the current configuration as a JSON string.
func (p *PolicyDemoteIdle) GetConfigJSON() string {
	if p.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(p.config); err == nil {
		return string(configStr)
	}
	return ""
}

// PidWatcher returns the associated PidWatcher.
func (p *PolicyDemoteIdle) PidWatcher() PidWatcher {
	return p.pidwatcher
}

// Mover returns the associated Mover.
func (p *PolicyDemoteIdle) Mover() *Mover {
	return p.mover
}

// Tracker returns nil; this policy has a built-in tracker.
func (p *PolicyDemoteIdle) Tracker() Tracker {
	return nil
}

// AddPids adds processes to track. Reads their address ranges from
// /proc/PID/maps and stores them as pages of interest.
func (p *PolicyDemoteIdle) AddPids(pids []int) {
	log.Debugf("PolicyDemoteIdle: AddPids(%v)\n", pids)
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for _, pid := range pids {
		ar, err := procMaps(pid)
		if err != nil {
			log.Debugf("PolicyDemoteIdle: failed to read maps for pid %d: %s\n", pid, err)
			continue
		}
		ranges := make([]pageRange, len(ar))
		for i, r := range ar {
			ranges[i] = pageRange{addr: r.addr, length: r.length}
		}
		sort.Slice(ranges, func(i, j int) bool {
			return ranges[i].addr < ranges[j].addr
		})
		merged := mergePageRanges(ranges)
		var totalPages uint64
		for _, r := range merged {
			totalPages += r.length
		}
		p.poi[pid] = merged
		log.Debugf("PolicyDemoteIdle: pid %d: %d ranges, %d pages (%.1f MiB)\n",
			pid, len(merged), totalPages, float64(totalPages)*float64(constUPagesize)/float64(1024*1024))
	}
}

// RemovePids removes processes from tracking. nil removes all.
func (p *PolicyDemoteIdle) RemovePids(pids []int) {
	log.Debugf("PolicyDemoteIdle: RemovePids(%v)\n", pids)
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if pids == nil {
		p.poi = make(map[int][]pageRange)
		return
	}
	for _, pid := range pids {
		delete(p.poi, pid)
	}
}

// Dump returns a summary of the policy state.
func (p *PolicyDemoteIdle) Dump(args []string) string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	totalRanges := 0
	totalPages := uint64(0)
	for _, ranges := range p.poi {
		totalRanges += len(ranges)
		for _, r := range ranges {
			totalPages += r.length
		}
	}
	return fmt.Sprintf("pids: %d, ranges: %d, pages: %d (%.1f MiB)",
		len(p.poi), totalRanges, totalPages,
		float64(totalPages)*float64(constUPagesize)/float64(1024*1024))
}

// Start starts the policy.
func (p *PolicyDemoteIdle) Start() error {
	if p.stop != nil {
		return fmt.Errorf("already started")
	}
	if p.config == nil {
		return fmt.Errorf("unconfigured policy")
	}
	if p.pidwatcher == nil && p.config.PagesOfInterestDir == "" {
		return fmt.Errorf("missing pidwatcher and pagesofinterestdir")
	}
	if p.pidwatcher != nil {
		p.pidwatcher.SetPidListener(p)
		if err := p.pidwatcher.Start(); err != nil {
			return fmt.Errorf("pidwatcher start error: %w", err)
		}
	}
	if err := p.mover.Start(); err != nil {
		return fmt.Errorf("mover start error: %w", err)
	}
	p.stop = make(chan struct{})
	go p.loop()
	return nil
}

// Stop stops the policy.
func (p *PolicyDemoteIdle) Stop() {
	if p.pidwatcher != nil {
		p.pidwatcher.Stop()
	}
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
	if p.mover != nil {
		p.mover.Stop()
	}
}

func (p *PolicyDemoteIdle) loop() {
	log.Debugf("PolicyDemoteIdle: online\n")
	defer log.Debugf("PolicyDemoteIdle: offline\n")
	ticker := time.NewTicker(time.Duration(p.config.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	destNode := Node(p.config.IdleNumas[0])

	for {
		stats.Store(StatsHeartbeat{"PolicyDemoteIdle.loop"})
		p.scanAndDemote(destNode)
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			continue
		}
	}
}

func (p *PolicyDemoteIdle) scanAndDemote(destNode Node) {
	usePOIDir := p.config.PagesOfInterestDir != ""

	type pidSnapshot struct {
		pid    int
		ranges []pageRange
	}
	var snapshots []pidSnapshot

	if usePOIDir {
		poiMap, err := p.readPOIDir()
		if err != nil {
			log.Debugf("PolicyDemoteIdle: readPOIDir error: %s\n", err)
			return
		}
		snapshots = make([]pidSnapshot, 0, len(poiMap))
		for pid, ranges := range poiMap {
			snapshots = append(snapshots, pidSnapshot{pid, ranges})
		}
		log.Debugf("PolicyDemoteIdle: readPOIDir: %d pids\n", len(snapshots))
	} else {
		p.mutex.Lock()
		snapshots = make([]pidSnapshot, 0, len(p.poi))
		for pid, ranges := range p.poi {
			snapshots = append(snapshots, pidSnapshot{pid, ranges})
		}
		p.mutex.Unlock()
	}

	bmFile, err := ProcPageIdleBitmapOpen()
	if err != nil {
		log.Debugf("PolicyDemoteIdle: cannot open idle bitmap: %s\n", err)
		return
	}
	defer bmFile.Close()

	pmAttrs := uint64(PMPresentSet | PMExclusiveSet)

	for _, snap := range snapshots {
		pid := snap.pid
		ranges := snap.ranges
		if len(ranges) == 0 {
			continue
		}

		pmFile, err := ProcPagemapOpen(pid)
		if err != nil {
			log.Debugf("PolicyDemoteIdle: pid %d: cannot open pagemap: %s\n", pid, err)
			if !usePOIDir {
				p.mutex.Lock()
				delete(p.poi, pid)
				p.mutex.Unlock()
			}
			continue
		}

		// Collect idle page addresses for this pid.
		idlePages := []Page{}

		addrRanges := pageRangesToAddrRanges(ranges)
		err = pmFile.ForEachPage(addrRanges, pmAttrs, func(pagemapBits uint64, pageAddr uint64) int {
			pfn := pagemapBits & PM_PFN
			pageIdle, err := bmFile.GetIdle(pfn)
			if err != nil {
				return 0
			}
			if pageIdle {
				idlePages = append(idlePages, Page{addr: pageAddr})
			}
			return 0
		})
		if err != nil {
			log.Debugf("PolicyDemoteIdle: pid %d: ForEachPage scan error: %s\n", pid, err)
			if !usePOIDir {
				p.mutex.Lock()
				delete(p.poi, pid)
				p.mutex.Unlock()
			}
			pmFile.Close()
			continue
		}
		log.Debugf("PolicyDemoteIdle: pid %d: scanned %d ranges, found %d idle pages\n",
			pid, len(ranges), len(idlePages))

		// Demote idle pages.
		if len(idlePages) > 0 && p.mover.TaskCount() == 0 {
			pp := &Pages{pid: pid, pages: idlePages}
			if usePOIDir {
				// POI dir mode: trust external source, move all idle pages.
				task := NewMoverTask(pp, destNode)
				p.mover.AddTask(task)
				log.Debugf("PolicyDemoteIdle: pid %d: demoting %d idle pages to %s\n",
					pid, len(idlePages), destNode)
			} else {
				// PidWatcher mode: filter out pages already on target.
				ppFiltered := pp.NotOnNode(destNode)
				if ppFiltered != nil && len(ppFiltered.Pages()) > 0 {
					task := NewMoverTask(ppFiltered, destNode)
					p.mover.AddTask(task)
					movedRanges := pagesToPageRanges(ppFiltered)
					p.mutex.Lock()
					if current, ok := p.poi[pid]; ok {
						p.poi[pid] = subtractSorted(current, movedRanges)
					}
					p.mutex.Unlock()
					log.Debugf("PolicyDemoteIdle: pid %d: demoting %d idle pages to %s\n",
						pid, len(ppFiltered.Pages()), destNode)
				}
			}
		}

		// Set idle bits on all pages of interest so we can
		// detect access on the next round.
		alreadyIdle := map[uint64]struct{}{}
		err = pmFile.ForEachPage(addrRanges, pmAttrs, func(pagemapBits uint64, pageAddr uint64) int {
			pfn := pagemapBits & PM_PFN
			pfnFileOffset := pfn / 64 * 8
			if _, ok := alreadyIdle[pfnFileOffset]; !ok {
				if err := bmFile.SetIdleAll(pfn); err != nil {
					return -1
				}
				alreadyIdle[pfnFileOffset] = struct{}{}
			}
			return 0
		})
		if err != nil && !usePOIDir {
			p.mutex.Lock()
			delete(p.poi, pid)
			p.mutex.Unlock()
		}
		pmFile.Close()
	}
}

// readPOIDir reads pages of interest from .vab files in the configured
// directory. Each file is named <PID>.vab and contains a sequence of
// little-endian (start_addr, end_addr) uint64 pairs. A shared flock
// on the "lock" file in the directory synchronizes with external writers.
func (p *PolicyDemoteIdle) readPOIDir() (map[int][]pageRange, error) {
	dir := p.config.PagesOfInterestDir
	lockPath := filepath.Join(dir, "lock")
	lockFile, err := os.OpenFile(lockPath, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lockfile %q: %w", lockPath, err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK {
			log.Debugf("PolicyDemoteIdle: readPOIDir: lock busy, skipping round\n")
			return nil, nil
		}
		return nil, fmt.Errorf("flock LOCK_SH %q: %w", lockPath, err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("readdir %q: %w", dir, err)
	}

	result := make(map[int][]pageRange)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".vab") {
			continue
		}
		pidStr := strings.TrimSuffix(name, ".vab")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		buf, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			log.Debugf("PolicyDemoteIdle: readPOIDir: read %s: %s\n", name, err)
			continue
		}
		if len(buf)%16 != 0 {
			log.Debugf("PolicyDemoteIdle: readPOIDir: %s: size %d not multiple of 16\n", name, len(buf))
			continue
		}

		ranges := make([]pageRange, 0, len(buf)/16)
		for i := 0; i+16 <= len(buf); i += 16 {
			start := binary.LittleEndian.Uint64(buf[i:])
			end := binary.LittleEndian.Uint64(buf[i+8:])
			if end <= start {
				continue
			}
			ranges = append(ranges, pageRange{
				addr:   start,
				length: (end - start) / constUPagesize,
			})
		}
		sort.Slice(ranges, func(i, j int) bool {
			return ranges[i].addr < ranges[j].addr
		})
		merged := mergePageRanges(ranges)
		if len(merged) > 0 {
			result[pid] = merged
		}
	}
	return result, nil
}

// pageRangesToAddrRanges converts internal pageRange slice to []AddrRange
// for use with ForEachPage.
func pageRangesToAddrRanges(ranges []pageRange) []AddrRange {
	ar := make([]AddrRange, len(ranges))
	for i, r := range ranges {
		ar[i] = AddrRange{addr: r.addr, length: r.length}
	}
	return ar
}

// pagesToPageRanges converts a Pages object to sorted, merged pageRange slice.
func pagesToPageRanges(pp *Pages) []pageRange {
	pages := pp.Pages()
	if len(pages) == 0 {
		return nil
	}
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].addr < pages[j].addr
	})
	ranges := []pageRange{{addr: pages[0].addr, length: 1}}
	for _, pg := range pages[1:] {
		last := &ranges[len(ranges)-1]
		if pg.addr == last.addr+last.length*constUPagesize {
			last.length++
		} else {
			ranges = append(ranges, pageRange{addr: pg.addr, length: 1})
		}
	}
	return ranges
}

// mergePageRanges merges overlapping or adjacent ranges in a sorted slice.
func mergePageRanges(sorted []pageRange) []pageRange {
	if len(sorted) == 0 {
		return sorted
	}
	merged := []pageRange{sorted[0]}
	for _, r := range sorted[1:] {
		last := &merged[len(merged)-1]
		lastEnd := last.addr + last.length*constUPagesize
		if r.addr <= lastEnd {
			rEnd := r.addr + r.length*constUPagesize
			if rEnd > lastEnd {
				last.length = (rEnd - last.addr) / constUPagesize
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// subtractSorted removes 'remove' ranges from 'from' ranges.
// Both inputs must be sorted by addr and non-overlapping.
// Returns a new sorted, non-overlapping slice.
func subtractSorted(from, remove []pageRange) []pageRange {
	result := make([]pageRange, 0, len(from))
	ri := 0
	for _, f := range from {
		fStart := f.addr
		fEnd := f.addr + f.length*constUPagesize
		// Advance remove index past ranges that end before f starts.
		for ri < len(remove) && remove[ri].addr+remove[ri].length*constUPagesize <= fStart {
			ri++
		}
		// Process all remove ranges that overlap with f.
		cursor := fStart
		for j := ri; j < len(remove) && remove[j].addr < fEnd; j++ {
			rStart := remove[j].addr
			rEnd := remove[j].addr + remove[j].length*constUPagesize
			if rStart > cursor {
				// Keep the gap before this remove range.
				result = append(result, pageRange{
					addr:   cursor,
					length: (rStart - cursor) / constUPagesize,
				})
			}
			if rEnd > cursor {
				cursor = rEnd
			}
		}
		// Keep any remaining part after the last remove range.
		if cursor < fEnd {
			result = append(result, pageRange{
				addr:   cursor,
				length: (fEnd - cursor) / constUPagesize,
			})
		}
	}
	return result
}

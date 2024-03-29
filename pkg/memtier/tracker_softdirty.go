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

// The soft dirty tracker is capable of detecting memory writes.
// https://www.kernel.org/doc/Documentation/vm/soft-dirty.txt

package memtier

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

// TrackerSoftDirtyConfig represents the configuration for TrackerSoftDirty.
type TrackerSoftDirtyConfig struct {
	// PagesInRegion is the number of pages in every address range
	// that is being watched and moved from a NUMA node to another.
	PagesInRegion uint64
	// MaxCountPerRegion is the maximum number of pages that are
	// reported to be written. When the maximum number is reached
	// during scanning a region, the rest of the pages in the
	// region are skipped. Value 0 means unlimited (that is, the
	// maximum number will be at most the same as PagesInRegion).
	MaxCountPerRegion uint64
	// ScanIntervalMs defines page scan interval in milliseconds.
	ScanIntervalMs uint64
	// RegionsUpdateMs defines process memory region update
	// interval in milliseconds. Regions are updated just before
	// scanning pages if the interval has passed. Value 0 means
	// that regions are updated before every scan.
	RegionsUpdateMs uint64
	// SkipPageProb enables sampling instead of reading through
	// pages in a region. Value 0 reads all pages as far as
	// MaxCountPerRegion is not reached. Value 1000 skips the next
	// page with probability 1.0, resulting in reading only the
	// first pages of every address range.
	SkipPageProb int
	// PagemapReadahead optimizes performance for the platform, if
	// 0 (undefined) use a default, if -1, disable readahead.
	PagemapReadahead int
	// EXPERIMENTAL (does not work):
	TrackReferenced bool // Track /proc/kpageflags PKF_REFERENCED bit.
}

// TODO: Referenced tracking does not work properly.
// TODO: if PFNs are tracked, refuse to start or disable if enabled
// /proc/sys/kernel/numa_balancing
const trackerSoftDirtyDefaults string = `{"PagesInRegion":512,"MaxCountPerRegion":1,"ScanIntervalMs":5000,"RegionsUpdateMs":10000}`

type accessCounter struct {
	a uint64 // number of times pages getting accessed
	w uint64 // number of times pages getting written
}

// TrackerSoftDirty is a memory tracker that detects memory writes.
type TrackerSoftDirty struct {
	mutex        sync.Mutex
	regionsMutex sync.Mutex
	config       *TrackerSoftDirtyConfig
	regions      map[int][]*AddrRanges
	// accesses maps pid -> startAddr -> lengthPages -> num of access & writes
	accesses  map[int]map[uint64]map[uint64]*accessCounter
	toSampler chan byte
	raes      rawAccessEntries
}

func init() {
	TrackerRegister("softdirty", NewTrackerSoftDirty)
}

// NewTrackerSoftDirty creates a new instance of TrackerSoftDirty.
func NewTrackerSoftDirty() (Tracker, error) {
	if !procFileExists("/proc/self/clear_refs") {
		return nil, fmt.Errorf("no platform support: /proc/pid/clear_refs missing")
	}
	t := &TrackerSoftDirty{
		regions:  make(map[int][]*AddrRanges),
		accesses: make(map[int]map[uint64]map[uint64]*accessCounter),
	}
	err := t.SetConfigJSON(trackerSoftDirtyDefaults)
	if err != nil {
		return nil, fmt.Errorf("invalid softdirty default configuration")
	}
	return t, nil
}

// SetConfigJSON sets the configuration for TrackerSoftDirty from a JSON string.
func (t *TrackerSoftDirty) SetConfigJSON(configJSON string) error {
	config := &TrackerSoftDirtyConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	t.config = config
	return nil
}

// GetConfigJSON returns the JSON representation of the TrackerSoftDirty's configuration.
func (t *TrackerSoftDirty) GetConfigJSON() string {
	if t.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(t.config); err == nil {
		return string(configStr)
	}
	return ""
}

func (t *TrackerSoftDirty) addRanges(pid int) {
	t.regions[pid] = []*AddrRanges{}
	p := NewProcess(pid)
	if ar, err := p.AddressRanges(); err == nil {
		// filter out single-page address ranges
		ar = ar.Filter(func(r AddrRange) bool { return r.Length() > 1 })
		ar = ar.SplitLength(t.config.PagesInRegion)
		t.regions[pid] = append(t.regions[pid], ar.Flatten()...)
	} else {
		delete(t.regions, pid)
		t.mutex.Lock()
		delete(t.accesses, pid)
		t.mutex.Unlock()
	}
}

// AddPids adds PIDs to the TrackerSoftDirty's regions map.
func (t *TrackerSoftDirty) AddPids(pids []int) {
	log.Debugf("TrackerSoftDirty: AddPids(%v)\n", pids)
	for _, pid := range pids {
		t.clearPageBits(pid)
		t.regionsMutex.Lock()
		t.addRanges(pid)
		t.regionsMutex.Unlock()
	}
}

// RemovePids removes PIDs from the TrackerSoftDirty's regions map.
func (t *TrackerSoftDirty) RemovePids(pids []int) {
	log.Debugf("TrackerSoftDirty: RemovePids(%v)\n", pids)
	if pids == nil {
		t.regionsMutex.Lock()
		t.regions = make(map[int][]*AddrRanges, 0)
		t.regionsMutex.Unlock()
		return
	}
	for _, pid := range pids {
		t.removePid(pid)
	}
}

func (t *TrackerSoftDirty) removePid(pid int) {
	t.regionsMutex.Lock()
	delete(t.regions, pid)
	t.regionsMutex.Unlock()
	t.mutex.Lock()
	delete(t.accesses, pid)
	t.mutex.Unlock()
}

// ResetCounters resets the counters that TrackerSoftDirty uses to track memory accesses.
func (t *TrackerSoftDirty) ResetCounters() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.accesses = make(map[int]map[uint64]map[uint64]*accessCounter)
}

// GetCounters returns the counters tracked by TrackerSoftDirty.
func (t *TrackerSoftDirty) GetCounters() *TrackerCounters {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	tcs := &TrackerCounters{}
	for pid, addrLenCount := range t.accesses {
		for start, lenCount := range addrLenCount {
			for length, accessCounts := range lenCount {
				addrRange := AddrRanges{
					pid: pid,
					addrs: []AddrRange{
						{
							addr:   start,
							length: length,
						},
					},
				}
				tc := TrackerCounter{
					Accesses: accessCounts.a,
					Reads:    0,
					Writes:   accessCounts.w,
					AR:       &addrRange,
				}
				*tcs = append(*tcs, tc)
			}
		}
	}
	return tcs
}

// Start starts the TrackerSoftDirty's sampler.
func (t *TrackerSoftDirty) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.toSampler != nil {
		return fmt.Errorf("sampler already running")
	}
	t.toSampler = make(chan byte, 1)
	t.clearPageBitsForRegionPids()
	go t.sampler()
	return nil
}

// Stop stops the TrackerSoftDirty's sampler.
func (t *TrackerSoftDirty) Stop() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.toSampler != nil {
		t.toSampler <- 0
	}
}

func (t *TrackerSoftDirty) sampler() {
	log.Debugf("TrackerSoftDirty: online\n")
	defer log.Debugf("TrackerSoftDirty: offline\n")
	ticker := time.NewTicker(time.Duration(t.config.ScanIntervalMs) * time.Millisecond)
	defer ticker.Stop()
	lastRegionsUpdateNs := time.Now().UnixNano()
	for {
		stats.Store(StatsHeartbeat{"TrackerSoftDirty.sampler"})
		select {
		case <-t.toSampler:
			t.mutex.Lock()
			close(t.toSampler)
			t.toSampler = nil
			t.mutex.Unlock()
			return
		case <-ticker.C:
			currentNs := time.Now().UnixNano()
			if time.Duration(currentNs-lastRegionsUpdateNs) >= time.Duration(t.config.RegionsUpdateMs)*time.Millisecond {
				t.regionsMutex.Lock()
				for pid := range t.regions {
					t.addRanges(pid)
				}
				t.regionsMutex.Unlock()
				lastRegionsUpdateNs = currentNs
			}
			t.countPages()
			t.clearPageBitsForRegionPids()
		}
	}
}

func (t *TrackerSoftDirty) countPages() {
	var kpfFile *ProcKpageflagsFile
	var err error

	t.mutex.Lock()
	trackReferenced := t.config.TrackReferenced
	maxCount := t.config.MaxCountPerRegion
	if maxCount == 0 {
		maxCount = t.config.PagesInRegion
	}
	skipPageProb := t.config.SkipPageProb
	pagemapReadahead := t.config.PagemapReadahead
	t.mutex.Unlock()

	cntPagesAccessed := uint64(0)
	cntPagesWritten := uint64(0)

	totScanned := uint64(0)

	if trackReferenced {
		// Referenced bits are in /proc/kpageflags.
		// Open the file already.
		kpfFile, err = ProcKpageflagsOpen()
		if err != nil {
			return
		}
		defer kpfFile.Close()
	}

	// pageHandler is called for all matching pages in the pagemap.
	// It counts number of pages accessed and written in a region.
	// The result is stored to cntPagesAccessed and cntPagesWritten.
	pageHandler := func(pagemapBits uint64, pageAddr uint64) int {
		totScanned++
		if pagemapBits&PM_SOFT_DIRTY == PM_SOFT_DIRTY {
			cntPagesWritten++
		}
		if trackReferenced {
			pfn := pagemapBits & PM_PFN
			flags, err := kpfFile.ReadFlags(pfn)
			if err != nil {
				return -1
			}
			if flags&KPF_REFERENCED == KPF_REFERENCED {
				cntPagesAccessed++
			}
		}
		// If we have exceeded the max count per region on the
		// counters we are tracking, stop reading pages further.
		if (cntPagesWritten > maxCount) &&
			(!trackReferenced || cntPagesAccessed > maxCount) {
			return -1
		}
		if skipPageProb > 0 {
			// skip pages in sampling read
			if skipPageProb >= 1000 {
				return -1
			}
			n := 0
			for rand.Intn(1000) < skipPageProb {
				n++
			}
			return n
		}
		return 0
	}
	t.regionsMutex.Lock()
	regions := make(map[int]*[]*AddrRanges, len(t.regions))
	for pid, allPidAddrRanges := range t.regions {
		regions[pid] = &allPidAddrRanges
	}
	t.regionsMutex.Unlock()

	t.processPagemapForSoftDirtyRegions(regions, pagemapReadahead, maxCount, &totScanned, &cntPagesAccessed, &cntPagesWritten, pageHandler)
}

func (t *TrackerSoftDirty) processPagemapForSoftDirtyRegions(regions map[int]*[]*AddrRanges, pagemapReadahead int, maxCount uint64, totScanned *uint64,
	cntPagesAccessed *uint64, cntPagesWritten *uint64, pageHandler func(pagemapBits uint64, pageAddr uint64) int) {
	pmAttrs := PMPresentSet | PMExclusiveSet

	totAccessed := uint64(0)
	totWritten := uint64(0)
	scanStartTime := time.Now().UnixNano()
	for pid, pAllPidAddrRanges := range regions {
		*totScanned = 0
		totAccessed = 0
		totWritten = 0
		pmFile, err := ProcPagemapOpen(pid)
		if err != nil {
			t.removePid(pid)
			continue
		}
		if pagemapReadahead > 0 {
			pmFile.SetReadahead(pagemapReadahead)
		}
		if pagemapReadahead == -1 {
			pmFile.SetReadahead(0)
		}
		for _, addrRanges := range *pAllPidAddrRanges {
			*cntPagesAccessed = 0
			*cntPagesWritten = 0

			err := pmFile.ForEachPage(addrRanges.Ranges(), pmAttrs, pageHandler)
			if err != nil {
				t.removePid(pid)
				break
			}
			if *cntPagesAccessed > maxCount {
				*cntPagesAccessed = maxCount
			}
			if *cntPagesWritten > maxCount {
				*cntPagesWritten = maxCount
			}
			t.mutex.Lock()
			addrLenCounts, ok := t.accesses[pid]
			if !ok {
				addrLenCounts = make(map[uint64]map[uint64]*accessCounter)
				t.accesses[pid] = addrLenCounts
			}
			addr := addrRanges.Ranges()[0].Addr()
			lenCounts, ok := addrLenCounts[addr]
			if !ok {
				lenCounts = make(map[uint64]*accessCounter)
				addrLenCounts[addr] = lenCounts
			}
			lengthPages := addrRanges.Ranges()[0].Length()
			counts, ok := lenCounts[lengthPages]
			if !ok {
				counts = &accessCounter{0, 0}
				lenCounts[lengthPages] = counts
			}
			counts.a += *cntPagesAccessed
			counts.w += *cntPagesWritten
			t.mutex.Unlock()
			totAccessed += *cntPagesAccessed
			totWritten += *cntPagesWritten
			if t.raes.data != nil {
				rae := &rawAccessEntry{
					timestamp: scanStartTime,
					pid:       pid,
					addr:      addr,
					length:    lengthPages,
					accessCounter: accessCounter{
						a: *cntPagesAccessed,
						w: *cntPagesWritten,
					},
				}
				t.raes.store(rae)
			}
		}
		pmFile.Close()
		scanEndTime := time.Now().UnixNano()
		stats.Store(StatsPageScan{
			pid:      pid,
			scanned:  *totScanned,
			accessed: totAccessed,
			written:  totWritten,
			timeUs:   (scanEndTime - scanStartTime) / int64(time.Microsecond),
		})
		scanStartTime = scanEndTime
	}
}

func (t *TrackerSoftDirty) regionsPids() []int {
	t.regionsMutex.Lock()
	defer t.regionsMutex.Unlock()
	pids := make([]int, 0, len(t.regions))
	for pid := range t.regions {
		pids = append(pids, pid)
	}
	return pids
}

func (t *TrackerSoftDirty) clearPageBits(pid int) {
	var err error
	pidString := strconv.Itoa(pid)
	path := "/proc/" + pidString + "/clear_refs"
	err = procWrite(path, []byte("4\n"))
	if t.config.TrackReferenced && err == nil {
		err = procWrite(path, []byte("1\n"))
	}
	if err != nil {
		// This process cannot be tracked anymore, remove it.
		t.removePid(pid)
	}
}

func (t *TrackerSoftDirty) clearPageBitsForRegionPids() {
	for _, pid := range t.regionsPids() {
		t.clearPageBits(pid)
	}
}

// Dump generates a dump based on the provided arguments.
func (t *TrackerSoftDirty) Dump(args []string) string {
	usage := "Usage: dump raw PARAMS"
	if len(args) == 0 {
		return usage
	}
	if args[0] == "raw" {
		return t.raes.dump(args[1:])
	}
	return ""
}

// Copyright 2024 Intel Corporation. All Rights Reserved.
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

// The finder tracker keeps reporting all memory address ranges but
// does not track accesses. This is useful, for instance, when all
// detected memory should be handled similarly by the policy.

package memtier

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// TrackerFinderConfig represents the configuration for TrackerFinder.
type TrackerFinderConfig struct {
	// RegionsUpdateMs defines process memory region update
	// interval in milliseconds. Regions are updated just before
	// scanning pages if the interval has passed. Value 0 means
	// that regions are updated before every scan.
	RegionsUpdateMs uint64
	// ReportAccesses is the number of accesses the tracker
	// reports on every address range it has found.
	ReportAccesses uint64
	// PagemapBitBIT filters in pages with only given values
	// of pagemap bits. If nil, there is no filtering.
	// If all PagemapBitBITs are nil, finding memory regions
	// is much faster because any pagemap bits are not checked.
	// For more information on pagemap bits, refer to:
	// https://www.kernel.org/doc/Documentation/vm/pagemap.txt
	PagemapBitExclusive *bool
	PagemapBitPresent   *bool
	PagemapBitSoftDirty *bool
}

const trackerFinderDefaults string = `{"RegionsUpdateMs":5000}`

// TrackerFinder is a memory tracker that reports address ranges
// without any accesses on them.
type TrackerFinder struct {
	mutex        sync.Mutex
	regionsMutex sync.Mutex
	config       *TrackerFinderConfig
	regions      map[int]*AddrRanges
	toSampler    chan byte
}

func init() {
	TrackerRegister("finder", NewTrackerFinder)
}

// NewTrackerFinder creates a new instance of TrackerFinder.
func NewTrackerFinder() (Tracker, error) {
	t := &TrackerFinder{
		regions: make(map[int]*AddrRanges),
	}
	err := t.SetConfigJSON(trackerFinderDefaults)
	if err != nil {
		return nil, fmt.Errorf("invalid finder default configuration")
	}
	return t, nil
}

// SetConfigJSON sets the configuration for TrackerFinder from a JSON string.
func (t *TrackerFinder) SetConfigJSON(configJSON string) error {
	config := &TrackerFinderConfig{}
	if err := UnmarshalConfig(configJSON, config); err != nil {
		return err
	}
	t.config = config
	return nil
}

// GetConfigJSON returns the JSON representation of the TrackerFinder's configuration.
func (t *TrackerFinder) GetConfigJSON() string {
	if t.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(t.config); err == nil {
		return string(configStr)
	}
	return ""
}

func (t *TrackerFinder) addRanges(pid int) {
	delete(t.regions, pid)
	p := NewProcess(pid)
	if ars, err := p.AddressRanges(); err == nil {
		t.regions[pid] = ars
	}
}

// AddPids adds PIDs to the TrackerFinder's regions map.
func (t *TrackerFinder) AddPids(pids []int) {
	log.Debugf("TrackerFinder: AddPids(%v)\n", pids)
	for _, pid := range pids {
		t.regionsMutex.Lock()
		t.addRanges(pid)
		t.regionsMutex.Unlock()
	}
}

// RemovePids removes PIDs from the TrackerFinder's regions map.
func (t *TrackerFinder) RemovePids(pids []int) {
	log.Debugf("TrackerFinder: RemovePids(%v)\n", pids)
	if pids == nil {
		t.regionsMutex.Lock()
		t.regions = make(map[int]*AddrRanges, 0)
		t.regionsMutex.Unlock()
		return
	}
	for _, pid := range pids {
		t.removePid(pid)
	}
}

func (t *TrackerFinder) removePid(pid int) {
	t.regionsMutex.Lock()
	delete(t.regions, pid)
	t.regionsMutex.Unlock()
}

// ResetCounters does nothing as TrackerFinder tracks no memory accesses.
func (t *TrackerFinder) ResetCounters() {
}

// GetCounters returns the counters tracked by TrackerFinder.
func (t *TrackerFinder) GetCounters() *TrackerCounters {
	t.regionsMutex.Lock()
	defer t.regionsMutex.Unlock()
	pageAttributes := uint64(0)
	if t.config.PagemapBitPresent != nil {
		if *t.config.PagemapBitPresent {
			pageAttributes |= PMPresentSet
		} else {
			pageAttributes |= PMPresentCleared
		}
	}
	if t.config.PagemapBitExclusive != nil {
		if *t.config.PagemapBitExclusive {
			pageAttributes |= PMExclusiveSet
		} else {
			pageAttributes |= PMExclusiveCleared
		}
	}
	if t.config.PagemapBitSoftDirty != nil {
		if *t.config.PagemapBitSoftDirty {
			pageAttributes |= PMDirtySet
		} else {
			pageAttributes |= PMDirtyCleared
		}
	}
	tcs := &TrackerCounters{}
	for _, addrRanges := range t.regions {
		ars := addrRanges
		if pageAttributes != 0 {
			scanStartTime := time.Now().UnixNano()
			if matching, err := ars.AddrRangesMatching(pageAttributes); err == nil {
				ars = matching
				scanEndTime := time.Now().UnixNano()
				stats.Store(StatsPageScan{
					pid:     ars.pid,
					scanned: ars.PageCount(),
					timeUs:  (scanEndTime - scanStartTime) / int64(time.Microsecond),
				})
			} else {
				// If reading pagemap bits in address ranges
				// failed, the process is probably already
				// dead. Then it does not make change to add
				// anything to the counters.
				// Clear the list of ranges.
				ars = &AddrRanges{pid: ars.pid}
			}
		}
		for _, ar := range ars.Ranges() {
			addrRange := AddrRanges{
				pid:   ars.pid,
				addrs: []AddrRange{ar},
			}
			tc := TrackerCounter{
				Accesses: t.config.ReportAccesses,
				Reads:    0,
				Writes:   0,
				AR:       &addrRange,
			}
			*tcs = append(*tcs, tc)
		}
	}
	return tcs
}

// Start starts the TrackerFinder's sampler.
func (t *TrackerFinder) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.toSampler != nil {
		return fmt.Errorf("sampler already running")
	}
	t.toSampler = make(chan byte, 1)
	go t.sampler()
	return nil
}

// Stop stops the TrackerFinder's sampler.
func (t *TrackerFinder) Stop() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.toSampler != nil {
		t.toSampler <- 0
	}
}

func (t *TrackerFinder) sampler() {
	log.Debugf("TrackerFinder: online\n")
	defer log.Debugf("TrackerFinder: offline\n")
	var ticker *time.Ticker
	if t.config.RegionsUpdateMs > 0 {
		ticker = time.NewTicker(time.Duration(t.config.RegionsUpdateMs) * time.Millisecond)
		defer ticker.Stop()
	} else {
		ticker = &time.Ticker{
			C: make(chan time.Time),
		}
	}
	for {
		stats.Store(StatsHeartbeat{"TrackerFinder.sampler"})
		select {
		case <-t.toSampler:
			t.mutex.Lock()
			close(t.toSampler)
			t.toSampler = nil
			t.mutex.Unlock()
			return
		case <-ticker.C:
			t.regionsMutex.Lock()
			for pid := range t.regions {
				t.addRanges(pid)
			}
			t.regionsMutex.Unlock()
		}
	}
}

// Dump generates a dump based on the provided arguments.
func (t *TrackerFinder) Dump(args []string) string {
	usage := "Usage: dump regions"
	if len(args) == 0 {
		return usage
	}
	if args[0] == "regions" {
		t.regionsMutex.Lock()
		defer t.regionsMutex.Unlock()
		return fmt.Sprintf("%v\n", t.regions)
	}
	return ""
}

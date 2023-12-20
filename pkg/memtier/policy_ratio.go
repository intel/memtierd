// Copyright 2023 Intel Corporation. All Rights Reserved.
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
	"time"
)

// PolicyRatioConfig represents the configuration for ratio policy, among which
// Ratio means the the portion of the least recently used memory, which
// should be swapped out when RatioTargets is missing or [-1], or moved to
// the target nodes according to RatioTargets
type PolicyRatioConfig struct {
	PidWatcher   PidWatcherConfig // mandantory
	Tracker      TrackerConfig    // optional
	Mover        MoverConfig      // optional
	IntervalMs   int              // optional
	Ratio        float32          // mandantory
	RatioTargets []int            // optional
	Cgroups      []string         // DEPRECATED, use PidWatcher "cgroup" instead
	Pids         []int            // DEPRECATED, use PidWatcher "pid" instead
}

// PolicyRatio is the main struct for the ratio policy, implementing the policy interface.
// the configuration, pid watchers, mover, pid and the addressc ranges, and so on, are included.
type PolicyRatio struct {
	config     *PolicyRatioConfig
	pidwatcher PidWatcher
	cgLoop     chan interface{}
	tracker    Tracker
	palt       *pidAddrLenTcRatio // pid - address - length - memory trackercounter's age
	mover      *Mover
	counter    uint64
}

type tcRatio struct {
	LastSeen    int64
	LastChanged int64
	LastRounds  uint64 // bitmap, i^th bit indicates if changed i rounds ago
	Tc          *TrackerCounter
}

type pidAddrLenTcRatio map[int]map[uint64]map[uint64]*tcRatio

func init() {
	PolicyRegister("ratio", NewPolicyRatio)
}

// NewPolicyRatio creates a new instance of PolicyRatio.
func NewPolicyRatio() (Policy, error) {
	p := &PolicyRatio{
		mover: NewMover(),
	}
	return p, nil
}

// SetConfigJSON sets the policy configuration.
func (p *PolicyRatio) SetConfigJSON(configJSON string) error {
	config := &PolicyRatioConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	return p.SetConfig(config)
}

// SetConfig checks the validity of the configuration and sets the policy configuration.
func (p *PolicyRatio) SetConfig(config *PolicyRatioConfig) error {
	// check "IntervalMs" configuration for ratio policy
	// optional field, defaults to 5 seconds if missing
	if config.IntervalMs == 0 {
		log.Debugf("IntervalMs missing from the ratio policy configuration, defaults to 5 seconds\n")
		config.IntervalMs = 5000
	}
	if config.IntervalMs < 0 {
		return fmt.Errorf("invalid age policy IntervalMs: %d, > 0 expected", config.IntervalMs)
	}

	// check "PidWatcher" configuration for ratio policy
	// mandatory field
	if len(config.Cgroups) > 0 {
		return deprecatedPolicyCgroupsConfig("ratio")
	}
	if len(config.Pids) > 0 {
		return deprecatedPolicyPidsConfig("ratio")
	}
	if config.PidWatcher.Name == "" {
		return fmt.Errorf("pidwatcher name missing from the ratio policy configuration")
	}
	newPidWatcher, err := NewPidWatcher(config.PidWatcher.Name)
	if err != nil {
		return err
	}
	if err = newPidWatcher.SetConfigJSON(config.PidWatcher.Config); err != nil {
		return fmt.Errorf("configuring pidwatcher %q for the ratio policy failed: %w", config.PidWatcher.Name, err)
	}
	p.pidwatcher = newPidWatcher

	// check "Ratio" configuration for ratio policy
	// mandatory field
	if config.Ratio <= 0 {
		return fmt.Errorf("invalid ratio policy Ratio: %f, > 0 expected", config.Ratio)
	}

	// check "RatioTargets" configuration for ratio policy
	// optional field, defaults to [-1], namely swapping out memory
	if len(config.RatioTargets) == 0 {
		log.Debugf("RatioTargets missing from the ratio policy configuration, defaults to [-1]")
		config.RatioTargets = []int{-1}
	}

	// check "Tracker" configuration for ratio policy
	// optional field, defaults to idlepage if missing
	if config.Tracker.Name == "" {
		log.Debugf("tracker name missing from the ratio policy configuration, defaults to idlepage\n")
		config.Tracker.Name = "idlepage"
	}
	newTracker, err := NewTracker(config.Tracker.Name)
	if err != nil {
		return err
	}
	if config.Tracker.Config != "" {
		if err = newTracker.SetConfigJSON(config.Tracker.Config); err != nil {
			return fmt.Errorf("configuring tracker %q for the ratio policy failed: %s", config.Tracker.Name, err)
		}
	}
	p.switchToTracker(newTracker)

	// check "mover" configuration for ratio policy
	// optional field, defaults to IntervalMs as 10 seconds and Bandwidth as 100 MB/s
	if err = p.mover.SetConfig(&config.Mover); err != nil {
		return fmt.Errorf("configuring mover failed: %s", err)
	}

	p.config = config
	return nil
}

// GetConfigJSON gets the policy configuration.
func (p *PolicyRatio) GetConfigJSON() string {
	if p.config == nil {
		return ""
	}
	pconfig := *p.config
	if p.tracker != nil {
		pconfig.Tracker.Config = p.tracker.GetConfigJSON()
	}
	if configStr, err := json.Marshal(&pconfig); err == nil {
		return string(configStr)
	}
	return ""
}

// switchToTracker stops the current tracker and initiates a new tracker.
func (p *PolicyRatio) switchToTracker(newTracker Tracker) {
	if p.tracker != nil {
		p.tracker.Stop()
	}
	p.tracker = newTracker
}

// PidWatcher returns pidwatcher for ratio policy.
func (p *PolicyRatio) PidWatcher() PidWatcher {
	return p.pidwatcher
}

// Mover returns mover for ratio policy.
func (p *PolicyRatio) Mover() *Mover {
	return p.mover
}

// Tracker returns tracker for ratio policy.
func (p *PolicyRatio) Tracker() Tracker {
	return p.tracker
}

// Dump is not implemented for ratio policy yet.
func (p *PolicyRatio) Dump(args []string) string {
	return ""
}

// Stop stops ratio policy.
func (p *PolicyRatio) Stop() {
	if p.pidwatcher != nil {
		p.pidwatcher.Stop()
	}
	if p.tracker != nil {
		p.tracker.Stop()
	}
	if p.cgLoop != nil {
		p.cgLoop <- struct{}{}
	}
	if p.mover != nil {
		p.mover.Stop()
	}
}

// Start starts ratio policy with given policy configuration.
func (p *PolicyRatio) Start() error {
	if p.cgLoop != nil {
		return fmt.Errorf("already started")
	}
	if p.config == nil {
		return fmt.Errorf("unconfigured policy")
	}
	if p.pidwatcher == nil {
		return fmt.Errorf("missing pidwatcher")
	}
	if p.tracker == nil {
		return fmt.Errorf("missing tracker")
	}
	if err := p.tracker.Start(); err != nil {
		return fmt.Errorf("tracker start error: %w", err)
	}
	p.pidwatcher.SetPidListener(p.tracker)
	if err := p.pidwatcher.Start(); err != nil {
		return fmt.Errorf("pidwatcher start error: %w", err)
	}
	if err := p.mover.Start(); err != nil {
		return fmt.Errorf("mover start error: %w", err)
	}
	p.cgLoop = make(chan interface{})
	go p.loop()
	return nil
}

// move moves the pages recorded in counters to the destNode.
func (p *PolicyRatio) move(tcs *TrackerCounters, destNode Node) {
	if p.mover.TaskCount() == 0 {
		for _, tc := range *tcs {
			ppages, err := tc.AR.PagesMatching(PMPresentSet | PMExclusiveSet)
			if err != nil {
				continue
			}
			ppages = ppages.NotOnNode(destNode)
			if len(ppages.Pages()) > 100 {
				task := NewMoverTask(ppages, destNode)
				p.mover.AddTask(task)
			}
		}
	}
}

// getBytesInTargetNuma returns how many bytes which is not located on the original node(s),
// this function will be called when moving memory among numa nodes.
func (p *PolicyRatio) getBytesInTargetNuma(pid int) (uint64, error) {
	// Obtain all the memory address ranges for the given pid
	process := NewProcess(pid)
	ar, err := process.AddressRanges()
	if err != nil {
		return 0, fmt.Errorf("error reading address ranges for process %d", pid)
	}
	if ar == nil {
		return 0, fmt.Errorf("address ranges not found for process %d", pid)
	}
	if len(ar.Ranges()) == 0 {
		return 0, fmt.Errorf("no address ranges from which to find pages for process %d", pid)
	}
	log.Debugf("found %d address ranges for process %d\n", len(ar.Ranges()), pid)

	// Match "Present" and "Exclusive" memory pages for the given pid
	pageAttributes, _ := parseOptPages("")
	pp, err := ar.PagesMatching(pageAttributes)
	if err != nil {
		return 0, fmt.Errorf("no address ranges from which to find present/exclusive pages for process %d", pid)
	}

	toNode := p.config.RatioTargets[0]
	pp = pp.OnNode(Node(toNode))
	return uint64(len(pp.pages)) * constUPagesize, nil
}

// getBytesInSwap returns the how many bytes which have been swapped out,
// this function will be called when swapping in and out memory.
func (p *PolicyRatio) getBytesInSwap(pid int) (uint64, error) {
	// Obtain all the memory address ranges for the given pid
	process := NewProcess(pid)
	ar, err := process.AddressRanges()
	if err != nil {
		return 0, fmt.Errorf("error reading address ranges for process %d", pid)
	}
	if ar == nil {
		return 0, fmt.Errorf("address ranges not found for process %d", pid)
	}
	if len(ar.Ranges()) == 0 {
		return 0, fmt.Errorf("no address ranges from which to find pages for process %d", pid)
	}
	log.Debugf("found %d address ranges for process %d\n", len(ar.Ranges()), pid)

	// Obtain the opened pagemap file for the given pid
	pmFile, err := ProcPagemapOpen(pid)
	if err != nil {
		return 0, fmt.Errorf("error ProcPagemapOpen for process %d", pid)
	}
	defer pmFile.Close()

	pages := 0
	swapped := uint64(0)
	_ = pmFile.ForEachPage(ar.Ranges(), 0,
		func(pmBits, pageAddr uint64) int {
			pages++
			if (pmBits>>PMB_SWAP)&1 == 0 { // Not Swap
				return 0
			}
			swapped++
			return 0
		})
	log.Debugf("getAddrRangesInSwapSpace %d / %d pages, %d / %d MB (%.1f %%) swapped out\n", swapped, pages,
		int64(swapped)*constPagesize/(1024*1024), int64(pages)*constPagesize/(1024*1024),
		float32(100*swapped)/float32(pages))
	return swapped * constUPagesize, nil
}

// getRatioCounters returns the counters recording the info of the corresponding pages
// which are the portion based on the ratio configuration of the least active.
func (p *PolicyRatio) getRatioCounters(timestamp int64, Ratio float32) *TrackerCounters {
	tcsMap := map[int]*TrackerCounters{}
	memSizeBytes := map[int]uint64{}
	for pid, alt := range *p.palt {
		tcs := &TrackerCounters{}
		_, ok := memSizeBytes[pid]
		if !ok {
			memSizeBytes[pid] = 0
		}
		for _, lt := range alt {
			for _, tcage := range lt {
				*tcs = append(*tcs, *tcage.Tc)
				for _, rng := range tcage.Tc.AR.addrs {
					memSizeBytes[pid] += rng.length * constUPagesize
				}
			}
		}
		list := tcsMap[pid]
		if list == nil {
			tcsMap[pid] = tcs
		} else {
			*tcsMap[pid] = append(*list, *tcs...)
		}
	}

	tcsList := &TrackerCounters{}
	for pid, tcs := range tcsMap {
		var moveSizeBytes uint64
		memSize := memSizeBytes[pid]
		tcs.SortByAccesses()
		idleTcs := &TrackerCounters{}

		if p.config.RatioTargets[0] == int(NodeSwap) {
			var err error
			moveSizeBytes, err = p.getBytesInSwap(pid)
			if err != nil {
				log.Errorf("getBytesInSwap error %v", err)
				continue
			}
		} else {
			var err error
			moveSizeBytes, err = p.getBytesInTargetNuma(pid)
			if err != nil {
				log.Errorf("getBytesInTargetNuma error %v", err)
				continue
			}
		}
		log.Debugf("counter a %v moved length: %v, total %v, ratio:%v\n", p.counter, moveSizeBytes, memSize, float32(moveSizeBytes)/float32(memSize))

		if len(*tcs) > 0 {
			for i := 0; i < len(*tcs) && moveSizeBytes < uint64(Ratio*float32(memSize)); i++ {
				addrsRange := *(*tcs)[i].AR
				for _, addr := range addrsRange.addrs {
					moveSizeBytes += addr.length * constUPagesize
				}
				*idleTcs = append(*idleTcs, (*tcs)[i])
			}
		}
		*tcsList = append(*tcsList, *idleTcs...)
		log.Debugf("counter b %v moved length: %v, total %v, ratio:%v\n", p.counter, moveSizeBytes, memSize, float32(moveSizeBytes)/float32(memSize))
	}

	return tcsList
}

// updateCounter updates the LastSeen, LastChanged and LastRounds of the counters,
// if the pid stored in the counter is found in pidAddrLenTcRatio, otherwise fill in the counter accordingly,
// which is called in the loop() every IntervalMs.
func (p *PolicyRatio) updateCounter(tc *TrackerCounter, timestamp int64) {
	pid := tc.AR.Pid()
	addr := tc.AR.Ranges()[0].Addr()
	length := tc.AR.Ranges()[0].Length()

	alt, ok := (*(p.palt))[pid]
	if !ok {
		alt = map[uint64]map[uint64]*tcRatio{}
		(*p.palt)[pid] = alt
	}
	lt, ok := alt[addr]
	if !ok {
		lt = map[uint64]*tcRatio{}
		alt[addr] = lt
	}
	prevTc, ok := lt[length]
	if !ok {
		copyOfTc := *tc
		prevTc = &tcRatio{
			LastSeen:    timestamp,
			LastChanged: timestamp,
			LastRounds:  1,
			Tc:          &copyOfTc,
		}
		lt[length] = prevTc
	} else {
		prevTc.LastSeen = timestamp
		prevTc.LastRounds = prevTc.LastRounds << 1
		if prevTc.Tc.Accesses != tc.Accesses ||
			prevTc.Tc.Reads != tc.Reads ||
			prevTc.Tc.Writes != tc.Writes {
			prevTc.LastChanged = timestamp
			prevTc.LastRounds |= 1
			prevTc.Tc.Accesses = tc.Accesses
			prevTc.Tc.Reads = tc.Reads
			prevTc.Tc.Writes = tc.Writes
		}
	}
}

// deleteDeadCounters deletes the counters which has the LastSeen at least happened 2*IntervalMs earlier
// or empty counters, this function is called in the loop() every IntervalMs.
func (p *PolicyRatio) deleteDeadCounters(timestamp int64) {
	aliveThreshold := timestamp - int64(2*time.Duration(p.config.IntervalMs)*time.Millisecond)
	for pid, alt := range *p.palt {
		for addr, lt := range alt {
			for length, tcRatio := range lt {
				if tcRatio.LastSeen < aliveThreshold {
					delete(lt, length)
				}
			}
			if len(lt) == 0 {
				delete(alt, addr)
			}
		}
		if len(alt) == 0 {
			delete(*p.palt, pid)
		}
	}
}

// loop will keep updating the counters from the tracker, and calculating the memory which is needed
// to be swapped out or moved to the target numa nodes according the Ratio and ratioTargets settings
// before memtierd stops the current ratio policy every IntervalMs.
func (p *PolicyRatio) loop() {
	log.Debugf("PolicyRatio: online\n")
	log.Debugf("Ratio config ratio: %v, targets: %v, interval: %v \n", p.config.Ratio, p.config.RatioTargets, p.config.IntervalMs)
	defer log.Debugf("PolicyRatio: offline\n")
	ticker := time.NewTicker(time.Duration(p.config.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	p.palt = &pidAddrLenTcRatio{}

	quit := false
	n := uint64(0) // for debugging
	for !quit {
		stats.Store(StatsHeartbeat{"PolicyRatio.loop"})
		timestamp := time.Now().UnixNano()

		// Before memtierd stops the current ratio policy, the tracker will keep getting counters,
		// updating counters, and delete dead counters if there are
		for _, tc := range *p.tracker.GetCounters() {
			p.updateCounter(&tc, timestamp)
		}
		p.deleteDeadCounters(timestamp)

		// According ratio portion in the policy, obtained the least active memory and move it afterwards
		idleTcs := p.getRatioCounters(timestamp, p.config.Ratio).RegionsMerged()
		var calcLen uint64
		if len(*idleTcs) > 0 {
			for _, tc := range *idleTcs {
				addrsRange := tc.AR
				for _, addr := range addrsRange.addrs {
					calcLen += addr.length
				}
			}
		}
		log.Debugf("Ratio loop %v moved length: %v\n", n, calcLen)

		// When the RatioTargets field is missing or set as [-1], swap out the memory calculated above
		// Otherwise, move the memory to the target numa nodes
		if len(p.config.RatioTargets) > 0 {
			p.move(idleTcs, Node(p.config.RatioTargets[0]))
		} else {
			p.move(idleTcs, NodeSwap)
		}

		n++           // for debugging
		p.counter = n // for debugging
		select {
		case <-p.cgLoop:
			quit = true
		case <-ticker.C:
			continue
		}
	}
	close(p.cgLoop)
	p.cgLoop = nil
}

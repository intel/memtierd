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
	"path/filepath"
	"regexp"
	"sync"
)

// PidWatcherFilterConfig holds the configuration for PidWatcherFilter.
type PidWatcherFilterConfig struct {
	Source  PidWatcherConfig
	Filters []*PidFilterConfig
}

// PidFilterConfig holds the configuration for a PidWatcherFilter filter.
type PidFilterConfig struct {
	ProcExeRegexp         string
	MinVmSizeKb           int
	MinVmRSSKb            int
	MinPrivateDirtyKb     int
	And                   []*PidFilterConfig
	Or                    []*PidFilterConfig
	Not                   *PidFilterConfig
	compiledProcExeRegexp *regexp.Regexp
}

// PidWatcherFilter is an implementation of PidWatcher that filters PIDs based on configured rules.
type PidWatcherFilter struct {
	config      *PidWatcherFilterConfig
	source      PidWatcher
	pidListener PidListener
	mutex       sync.Mutex
}

// FilteringPidListener is a listener for PidWatcherFilter that filters and forwards PID changes.
type FilteringPidListener struct {
	w         *PidWatcherFilter
	addedPids map[int]setMemberType
}

func init() {
	PidWatcherRegister("filter", NewPidWatcherFilter)
}

// NewPidWatcherFilter creates a new instance of PidWatcherFilter.
func NewPidWatcherFilter() (PidWatcher, error) {
	return &PidWatcherFilter{}, nil
}

// SetConfigJSON is a method of PidWatcherFilter that sets the configuration from JSON.
// It creates a new source PidWatcher based on the provided configuration.
func (w *PidWatcherFilter) SetConfigJSON(configJSON string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	config := &PidWatcherFilterConfig{}
	if err := unmarshal(configJSON, config); err != nil {
		return err
	}
	newSource, err := NewPidWatcher(config.Source.Name)
	if err != nil {
		return fmt.Errorf("pidwatcher filter failed to create source: %w", err)
	}
	if err = newSource.SetConfigJSON(config.Source.Config); err != nil {
		return fmt.Errorf("configuring pidwatcher filter's source pidwatcher %q failed: %w", config.Source.Name, err)
	}
	// Validate filters.
	for _, fc := range config.Filters {
		if err := prepareFilter(fc); err != nil {
			return fmt.Errorf("invalid filter: %s", err)
		}
	}
	w.source = newSource
	w.config = config
	f := &FilteringPidListener{
		addedPids: map[int]setMemberType{},
		w:         w,
	}
	if w.source != nil {
		w.source.SetPidListener(f)
	}
	return nil
}

// GetConfigJSON is a method of PidWatcherFilter that returns the current configuration as a JSON string.
func (w *PidWatcherFilter) GetConfigJSON() string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(w.config); err == nil {
		return string(configStr)
	}
	return ""
}

// SetPidListener is a method of PidWatcherFilter that sets the PidListener.
func (w *PidWatcherFilter) SetPidListener(l PidListener) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.pidListener = l
}

// Poll is a method of PidWatcherFilter that triggers the polling process of the source PidWatcher.
func (w *PidWatcherFilter) Poll() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.source == nil {
		return fmt.Errorf("pidwatcher filter: poll: missing pid source")
	}
	return w.source.Poll()
}

// Start is a method of PidWatcherFilter that starts the source PidWatcher.
func (w *PidWatcherFilter) Start() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.source == nil {
		return fmt.Errorf("pidwatcher filter: start: missing pid source")
	}
	return w.source.Start()
}

// Stop is a method of PidWatcherFilter that stops the source PidWatcher.
func (w *PidWatcherFilter) Stop() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.source == nil {
		return
	}
	w.source.Stop()
}

// Dump is a method of PidWatcherFilter that returns a string representation of the current instance.
func (w *PidWatcherFilter) Dump([]string) string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return fmt.Sprintf("%+v", w)
}

func prepareFilter(fc *PidFilterConfig) error {
	if fc.ProcExeRegexp != "" && fc.compiledProcExeRegexp == nil {
		re, err := regexp.Compile(fc.ProcExeRegexp)
		if err != nil {
			return fmt.Errorf("invalid ProcExeRegexp: %q: %w", fc.ProcExeRegexp, err)
		}
		fc.compiledProcExeRegexp = re
	}
	for _, childFc := range append(fc.And, fc.Or...) {
		if err := prepareFilter(childFc); err != nil {
			return err
		}
	}
	if fc.Not != nil {
		if err := prepareFilter(fc.Not); err != nil {
			return err
		}
	}
	return nil
}

func pidsMatchingFilter(fc *PidFilterConfig, pids []int) []int {
	matchingPids := pids
	if fc.compiledProcExeRegexp != nil {
		matchingPids = pidsMatchingFilterProcExeRegexp(fc, matchingPids)
	}
	if fc.MinVmSizeKb != 0 {
		matchingPids = pidsMatchingFilterMinVmSizeKb(fc, matchingPids)
	}
	if fc.MinVmRSSKb != 0 {
		matchingPids = pidsMatchingFilterMinVmRSSKb(fc, matchingPids)
	}
	if fc.MinPrivateDirtyKb != 0 {
		matchingPids = pidsMatchingFilterMinPrivateDirtyKb(fc, matchingPids)
	}
	if len(fc.And) > 0 {
		matchingPids = pidsMatchingFilterAnd(fc.And, matchingPids)
	}
	if len(fc.Or) > 0 {
		matchingPids = pidsMatchingFilterOr(fc.Or, matchingPids)
	}
	if fc.Not != nil {
		matchingPids = pidsMatchingFilterNot(fc.Not, matchingPids)
	}
	return matchingPids
}

func pidsMatchingFilterAnd(fcs []*PidFilterConfig, pids []int) []int {
	for _, fc := range fcs {
		if len(pids) == 0 {
			// short-circuit: all pids already filtered out
			break
		}
		pids = pidsMatchingFilter(fc, pids)
	}
	return pids
}

func pidsMatchingFilterOr(fcs []*PidFilterConfig, pids []int) []int {
	pidsInOr := map[int]setMemberType{}
	for _, fc := range fcs {
		for _, pid := range pidsMatchingFilter(fc, pids) {
			pidsInOr[pid] = setMember
		}
		if len(pidsInOr) == len(pids) {
			// short-circuit: all pids already matched
			break
		}
	}
	matchingPids := make([]int, 0, len(pidsInOr))
	for pid := range pidsInOr {
		matchingPids = append(matchingPids, pid)
	}
	return matchingPids
}

func pidsMatchingFilterNot(fc *PidFilterConfig, pids []int) []int {
	notPids := map[int]setMemberType{}
	for _, pid := range pidsMatchingFilter(fc, pids) {
		notPids[pid] = setMember
	}
	otherPids := make([]int, 0, len(pids)-len(notPids))
	for _, pid := range pids {
		if _, ok := notPids[pid]; !ok {
			otherPids = append(otherPids, pid)
		}
	}
	return otherPids
}

func pidsMatchingFilterMinVmSizeKb(fc *PidFilterConfig, pids []int) []int {
	matchingPids := []int{}
	for _, pid := range pids {
		vmSize, err := procReadIntFromLine(fmt.Sprintf("/proc/%d/status", pid), "VmSize:", 1)
		if err != nil {
			continue
		}
		if vmSize >= fc.MinVmSizeKb {
			matchingPids = append(matchingPids, pid)
		}
	}
	return matchingPids
}

func pidsMatchingFilterMinVmRSSKb(fc *PidFilterConfig, pids []int) []int {
	matchingPids := []int{}
	for _, pid := range pids {
		vmRSS, err := procReadIntFromLine(fmt.Sprintf("/proc/%d/status", pid), "VmRSS:", 1)
		if err != nil {
			continue
		}
		if vmRSS >= fc.MinVmRSSKb {
			matchingPids = append(matchingPids, pid)
		}
	}
	return matchingPids
}

func pidsMatchingFilterMinPrivateDirtyKb(fc *PidFilterConfig, pids []int) []int {
	matchingPids := []int{}
	for _, pid := range pids {
		privateDirty, err := procReadIntSumFromLines(fmt.Sprintf("/proc/%d/smaps", pid), "Private_Dirty", 1)
		if err != nil {
			continue
		}
		if privateDirty >= fc.MinPrivateDirtyKb {
			matchingPids = append(matchingPids, pid)
		}
	}
	return matchingPids
}

func pidsMatchingFilterProcExeRegexp(fc *PidFilterConfig, pids []int) []int {
	matchingPids := []int{}
	for _, pid := range pids {
		exeFilepath, err := filepath.EvalSymlinks(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			continue
		}
		if fc.compiledProcExeRegexp.MatchString(exeFilepath) {
			matchingPids = append(matchingPids, pid)
		}
	}
	return matchingPids
}

// AddPids is a method of FilteringPidListener that filters and forwards new PIDs.
func (f *FilteringPidListener) AddPids(pids []int) {
	f.w.mutex.Lock()
	defer f.w.mutex.Unlock()
	matchingPids := pidsMatchingFilterOr(f.w.config.Filters, pids)
	if len(matchingPids) == 0 {
		return
	}
	for _, pid := range matchingPids {
		f.addedPids[pid] = setMember
	}
	if f.w.pidListener != nil {
		f.w.pidListener.AddPids(matchingPids)
	} else {
		log.Warnf("pidwatcher filter: ignoring new pids %v because nobody is listening", matchingPids)
	}
}

// RemovePids is a method of FilteringPidListener that filters and forwards disappeared PIDs.
func (f *FilteringPidListener) RemovePids(pids []int) {
	f.w.mutex.Lock()
	defer f.w.mutex.Unlock()
	matchingPids := []int{}
	for _, pid := range pids {
		if _, ok := f.addedPids[pid]; ok {
			matchingPids = append(matchingPids, pid)
			delete(f.addedPids, pid)
		}
	}
	if len(matchingPids) == 0 {
		return
	}
	if f.w.pidListener != nil {
		f.w.pidListener.RemovePids(matchingPids)
	} else {
		log.Warnf("pidwatcher filter: ignoring disappeared pids %v because nobody is listening", matchingPids)
	}
}

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
	"strings"
)

// AddrData is a data structure for storing arbitrary data on memory
// addresses.
type AddrData struct {
	AddrRange
	data interface{}
}

// AddrDatas slice is sorted by non-overlapping address ranges.
type AddrDatas struct {
	ads []*AddrData
}

// NewAddrData returns new AddrData instance that associates data to
// an address range.
func NewAddrData(addr, length uint64, data interface{}) *AddrData {
	return &AddrData{
		AddrRange: AddrRange{
			addr:   addr,
			length: length,
		},
		data: data,
	}
}

// NewAddrDatas returns a data structure that contains many address
// ranges with different data.
func NewAddrDatas() *AddrDatas {
	return &AddrDatas{
		ads: []*AddrData{},
	}
}

// Data fetches data associated with an address.
// data, ok := ads.Data(addr)
// behaves similarly to
// value, ok := map[key]
func (a *AddrDatas) Data(addr uint64) (interface{}, bool) {
	first, count := a.overlapping(&AddrRange{addr, 1})
	if count > 0 && a.ads[first].addr <= addr {
		return a.ads[first].data, true
	}
	return nil, false
}

// ForEach iterates over addrdatas.
//   - handler(*AddrRange, data) is called for every entry
//     in the ascending start address order. Handler return values:
//     0 (continue): ForEach continues iteration from the next element
//     -1 (break):   ForEach returns immediately.
func (a *AddrDatas) ForEach(handler func(*AddrRange, interface{}) int) {
	for _, ad := range a.ads {
		next := handler(&ad.AddrRange, ad.data)
		switch next {
		case 0:
			continue
		case -1:
			return
		default:
			panic(fmt.Sprintf("illegal AddrDatas.ForEach handler return value %d", next))
		}
	}
}

// Dump returns a string containing address ranges with associated values.
func (a *AddrDatas) Dump() string {
	sl := []string{}
	for _, ad := range a.ads {
		firstPage := ad.addr / constUPagesize
		lastPage := firstPage + ad.length - 1
		if lastPage != firstPage {
			sl = append(sl, fmt.Sprintf("%d-%d:%v", firstPage, lastPage, ad.data))
		} else {
			sl = append(sl, fmt.Sprintf("%d:%v", firstPage, ad.data))
		}
	}
	return "AddrDatas{" + strings.Join(sl, ",") + "}"
}

// SetData associates data with an address range. If the address range
// overlaps with existing ranges with some other data, data associated
// with overlapping parts will be overwritten.
func (a *AddrDatas) SetData(ar AddrRange, data interface{}) {
	if ar.length <= 0 {
		return
	}
	ad := NewAddrData(ar.addr, ar.length, data)
	first, count := a.overlapping(&ad.AddrRange)
	last := first + count - 1
	newLen := len(a.ads) - count + 1
	if count > 0 {
		if a.ads[first].addr < ad.addr {
			newLen++
		}
		if a.ads[last].EndAddr() > ad.EndAddr() {
			newLen++
		}
	}
	newAds := make([]*AddrData, 0, newLen)
	newAds = append(newAds, a.ads[0:first]...)
	if count > 0 {
		if a.ads[first].addr < ad.addr {
			// Case:
			// |---------ads[first]--...
			//            |---ad-----...
			// =>
			// |---TBD----|---ad-----...
			var newFirst *AddrData
			if count == 1 && a.ads[first].EndAddr() > ad.EndAddr() {
				// Case: first == last, cannot reuse ads[first] twice
				// |---------ads[first]-----------|
				//            |---ad---|
				// =>
				// |-newFirst-|---ad---|ads[first]|
				newFirst = NewAddrData(a.ads[first].addr,
					a.ads[first].length,
					a.ads[first].data)
			} else {
				// Case: reuse ads[first]
				// |---------ads[first]--...
				//            |---ad-----...
				// =>
				// |ads[first]|---ad-----...
				newFirst = a.ads[first]
			}
			newFirst.length = (ad.addr - newFirst.addr) / constUPagesize
			newAds = append(newAds, newFirst)
		}
	}
	newAds = append(newAds, ad)
	if count > 0 {
		if a.ads[last].EndAddr() > ad.EndAddr() {
			a.ads[last].length = (a.ads[last].EndAddr() - ad.EndAddr()) / constUPagesize
			a.ads[last].addr = ad.EndAddr()
			newAds = append(newAds, a.ads[last])
		}
	}
	if last+1 < len(a.ads) {
		newAds = append(newAds, a.ads[last+1:]...)
	}
	a.ads = newAds
}

// Sorted returns a new slice of AddrData instances sorted based on the provided 'less' function.
// It does not modify the original AddrDatas; instead, it returns a sorted shallow copy.
func (a *AddrDatas) Sorted(less func(ad0, ad1 *AddrData) bool) []*AddrData {
	shallowCopyOfAds := make([]*AddrData, len(a.ads))
	copy(shallowCopyOfAds, a.ads)
	sort.Slice(shallowCopyOfAds, func(i, j int) bool {
		return less(shallowCopyOfAds[i], shallowCopyOfAds[j])
	})
	return shallowCopyOfAds
}

// overlapping returns index of the first overlapping address range
// and the number of overlapping address ranges in AddrDatas.
func (a *AddrDatas) overlapping(ar0 *AddrRange) (int, int) {
	first := sort.Search(len(a.ads), func(i int) bool { return a.ads[i].EndAddr() > ar0.addr })
	count := 0
	ar0EndAddr := ar0.EndAddr()
	for _, ad := range a.ads[first:] {
		if ar0EndAddr <= ad.addr {
			break
		}
		count++
	}
	return first, count
}

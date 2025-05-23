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
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

//nolint:golint //ignore the variables' naming as they are from Linux kernel code
const (
	// /proc/pid/pagemap bits
	// from fs/proc/task_mmu.c
	PMB_SOFT_DIRTY     = 55
	PMB_MMAP_EXCLUSIVE = 56
	PMB_UFFD_WP        = 57
	PMB_FILE           = 61
	PMB_SWAP           = 62
	PMB_PRESENT        = 63
	// corresponding bitmasks
	PM_PFN            = (uint64(0x1) << 55) - 1
	PM_SOFT_DIRTY     = uint64(0x1) << PMB_SOFT_DIRTY
	PM_MMAP_EXCLUSIVE = uint64(0x1) << PMB_MMAP_EXCLUSIVE
	PM_UFFD_WP        = uint64(0x1) << PMB_UFFD_WP
	PM_FILE           = uint64(0x1) << PMB_FILE
	PM_SWAP           = uint64(0x1) << PMB_SWAP
	PM_PRESENT        = uint64(0x1) << PMB_PRESENT

	// /proc/kpageflags bits
	// from include/uapi/linux/kernel-page-flags.h
	KPFB_LOCKED        = 0
	KPFB_ERROR         = 1
	KPFB_REFERENCED    = 2
	KPFB_UPTODATE      = 3
	KPFB_DIRTY         = 4
	KPFB_LRU           = 5
	KPFB_ACTIVE        = 6
	KPFB_SLAB          = 7
	KPFB_WRITEBACK     = 8
	KPFB_RECLAIM       = 9
	KPFB_BUDDY         = 10
	KPFB_MMAP          = 11
	KPFB_ANON          = 12
	KPFB_SWAPCACHE     = 13
	KPFB_SWAPBACKED    = 14
	KPFB_COMPOUND_HEAD = 15
	KPFB_COMPOUND_TAIL = 16
	KPFB_HUGE          = 17
	KPFB_UNEVICTABLE   = 18
	KPFB_HWPOISON      = 19
	KPFB_NOPAGE        = 20
	KPFB_KSM           = 21
	KPFB_THP           = 22
	KPFB_OFFLINE       = 23
	KPFB_ZERO_PAGE     = 24
	KPFB_IDLE          = 25
	KPFB_PGTABLE       = 26
	KPF_LOCKED         = uint64(0x1) << 0
	KPF_ERROR          = uint64(0x1) << 1
	KPF_REFERENCED     = uint64(0x1) << 2
	KPF_UPTODATE       = uint64(0x1) << 3
	KPF_DIRTY          = uint64(0x1) << 4
	KPF_LRU            = uint64(0x1) << 5
	KPF_ACTIVE         = uint64(0x1) << 6
	KPF_SLAB           = uint64(0x1) << 7
	KPF_WRITEBACK      = uint64(0x1) << 8
	KPF_RECLAIM        = uint64(0x1) << 9
	KPF_BUDDY          = uint64(0x1) << 10
	KPF_MMAP           = uint64(0x1) << 11
	KPF_ANON           = uint64(0x1) << 12
	KPF_SWAPCACHE      = uint64(0x1) << 13
	KPF_SWAPBACKED     = uint64(0x1) << 14
	KPF_COMPOUND_HEAD  = uint64(0x1) << 15
	KPF_COMPOUND_TAIL  = uint64(0x1) << 16
	KPF_HUGE           = uint64(0x1) << 17
	KPF_UNEVICTABLE    = uint64(0x1) << 18
	KPF_HWPOISON       = uint64(0x1) << 19
	KPF_NOPAGE         = uint64(0x1) << 20
	KPF_KSM            = uint64(0x1) << 21
	KPF_THP            = uint64(0x1) << 22
	KPF_OFFLINE        = uint64(0x1) << 23
	KPF_ZERO_PAGE      = uint64(0x1) << 24
	KPF_IDLE           = uint64(0x1) << 25
	KPF_PGTABLE        = uint64(0x1) << 26
)

// ProcMemFile represents a structure for managing a file associated with process memory.
type ProcMemFile struct {
	osFile  *os.File
	bufSize uint64
	pos     uint64
}

// ProcPagemapFile represents a structure for managing a file associated with proc pagemap.
type ProcPagemapFile struct {
	osFile    *os.File
	readahead int
	pos       int64
}

// ProcKpageflagsFile represents a structure for managing a file associated with kpage flags.
type ProcKpageflagsFile struct {
	osFile    *os.File
	readahead int
	readCache map[int64]uint64
}

// ProcPageIdleBitmapFile represents a structure for managing a file associated with proc page idle bitmaps.
type ProcPageIdleBitmapFile struct {
	osFile    *os.File
	readahead int
	readCache map[int64]uint64
}

func procFileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func procWrite(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}

func procWriteInt(path string, i int) error {
	return procWrite(path, []byte(strconv.Itoa(i)))
}

func procWriteUint64(path string, i uint64) error {
	return procWrite(path, []byte(strconv.FormatUint(i, 10)))
}

func procRead(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func procReadTrimmed(path string) (string, error) {
	s, err := procRead(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func procReadInt(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("read empty string, expected int from %q", path)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return n, nil
}

func procReadInts(path string) ([]int, error) {
	var ints []int
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for lineIndex, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		for fieldIndex, value := range fields {
			n, err := strconv.Atoi(fields[fieldIndex])
			if err != nil {
				return nil, fmt.Errorf("%s:%d error parsing int from field %d: %q", path, lineIndex+1, fieldIndex+1, value)
			}
			ints = append(ints, n)
		}
	}
	return ints, nil
}

func procReadIntFromLine(path string, linePrefix string, fieldIndex int) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for lineIndex, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, linePrefix) {
			fields := strings.Fields(line)
			if fieldIndex < len(fields) {
				n, err := strconv.Atoi(fields[fieldIndex])
				if err != nil {
					return 0, fmt.Errorf("%s:%d (prefix %q) error parsing int from field index %d", path, lineIndex+1, linePrefix, fieldIndex)
				}
				return n, nil
			}
			return 0, fmt.Errorf("%s:%d (prefix %q) line has only %d fields, cannot index with %d", path, lineIndex+1, linePrefix, len(fields), fieldIndex)
		}
	}
	return 0, fmt.Errorf("file %q has no line starting with prefix %q", path, linePrefix)
}

func procReadIntSumFromLines(path string, linePrefix string, fieldIndex int) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	sum := 0
	for lineIndex, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, linePrefix) {
			fields := strings.Fields(line)
			if fieldIndex < len(fields) {
				n, err := strconv.Atoi(fields[fieldIndex])
				if err != nil {
					return sum, fmt.Errorf("%s:%d (prefix %q) error parsing int from field index %d", path, lineIndex+1, linePrefix, fieldIndex)
				}
				sum += n
			} else {
				return 0, fmt.Errorf("%s:%d (prefix %q) line has only %d fields, cannot index with %d", path, lineIndex+1, linePrefix, len(fields), fieldIndex)
			}
		}
	}
	return sum, nil
}

// procReadIntListFormat parses List Formats used in cpuset.cpus and .mems.
// Example: "0-3,5,7-9" -> []int{0, 1, 2, 3, 5, 7, 8, 9}
func procReadIntListFormat(path string, defaultValue []int) ([]int, error) {
	data, err := procReadTrimmed(path)
	if err != nil {
		return nil, err
	}
	if data == "" {
		return defaultValue, nil
	}
	result := []int{}
	for _, comma := range strings.Split(data, ",") {
		rng := strings.Split(comma, "-")
		if len(rng) == 1 {
			n, err := strconv.Atoi(rng[0])
			if err != nil {
				return nil, err
			}
			result = append(result, n)
			continue
		}
		first, err := strconv.Atoi(rng[0])
		if err != nil {
			return nil, err
		}
		last, err := strconv.Atoi(rng[1])
		if err != nil {
			return nil, err
		}
		for n := first; n <= last; n++ {
			result = append(result, n)
		}
	}
	return result, nil
}

func ProcMemOpen(pid int) (*ProcMemFile, error) {
	path := fmt.Sprintf("/proc/%d/mem", pid)
	osFile, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return &ProcMemFile{osFile, 64 * constUPagesize, 0}, nil
}

func (f *ProcMemFile) Close() error {
	return f.osFile.Close()
}

// ReadNoData reads one byte from every page in the address range
// to force each page in memory.
func (f *ProcMemFile) ReadNoData(startAddr, endAddr uint64) error {
	buf := make([]byte, f.bufSize)
	for startAddr < endAddr {
		readLen := len(buf)
		if startAddr+uint64(readLen) > endAddr {
			readLen = int(endAddr - startAddr)
		}
		if f.pos != startAddr {
			if _, err := f.osFile.Seek(int64(startAddr), io.SeekStart); err != nil {
				f.pos = 0
				return err
			}
		}
		nbytes, err := io.ReadAtLeast(f.osFile, buf[:readLen], readLen)
		if err != nil {
			f.pos = 0
			return err
		}
		startAddr += uint64(nbytes)
		f.pos += uint64(nbytes)
	}
	return nil
}

func ProcKpageflagsOpen() (*ProcKpageflagsFile, error) {
	path := "/proc/kpageflags"
	osFile, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	return &ProcKpageflagsFile{osFile, 256, map[int64]uint64{}}, nil
}

func (f *ProcKpageflagsFile) SetReadahead(pages int) {
	f.readahead = pages
	if pages > 0 {
		f.readCache = map[int64]uint64{}
	} else {
		f.readCache = nil
	}
}

// ReadFlags returns 64-bit set of flags from /proc/kpageflags
// for a page indexed by page frame number (PFN).
func (f *ProcKpageflagsFile) ReadFlags(pfn uint64) (uint64, error) {
	kpfFileOffset := int64(pfn * 8)
	if f.readCache != nil {
		if flags, ok := f.readCache[kpfFileOffset]; ok {
			return flags, nil
		}
	}
	if _, err := f.osFile.Seek(kpfFileOffset, io.SeekStart); err != nil {
		return 0, err
	}
	readBufSize := 8 * (f.readahead + 1)
	readBuf := make([]byte, readBufSize)

	nbytes, err := io.ReadAtLeast(f.osFile, readBuf, readBufSize)
	if err != nil {
		return 0, err
	}
	if nbytes != readBufSize {
		return 0, fmt.Errorf("reading %d bytes from kpageflags failed, got %d bytes", readBufSize, nbytes)
	}
	flags := binary.LittleEndian.Uint64(readBuf)
	if f.readCache != nil {
		f.readCache[kpfFileOffset] = flags
		readBuf = readBuf[8:]
		for len(readBuf) > 0 {
			kpfFileOffset += 8
			flagsAhead := binary.LittleEndian.Uint64(readBuf[:8])
			f.readCache[kpfFileOffset] = flagsAhead
			readBuf = readBuf[8:]
		}
	}
	return flags, nil
}

func (f *ProcKpageflagsFile) Close() {
	f.osFile.Close()
}

// ProcPageIdleBitmapOpen returns opened page_idle/bitmap file
func ProcPageIdleBitmapOpen() (*ProcPageIdleBitmapFile, error) {
	path := "/sys/kernel/mm/page_idle/bitmap"
	osFile, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &ProcPageIdleBitmapFile{osFile, 8, map[int64]uint64{}}, nil
}

func (f *ProcPageIdleBitmapFile) Close() {
	f.osFile.Close()
	f.osFile = nil
}

func (f *ProcPageIdleBitmapFile) SetReadahead(chunks int) {
	f.readahead = chunks
	if chunks > 0 {
		f.readCache = map[int64]uint64{}
	} else {
		f.readCache = nil
	}
}

func (f *ProcPageIdleBitmapFile) SetIdle(pfn uint64) error {
	pfnBitOffset := pfn % 64
	idleMask := uint64(0x1) << pfnBitOffset
	return f.WriteBits(pfn, idleMask)
}

func (f *ProcPageIdleBitmapFile) SetIdleAll(pfn uint64) error {
	return f.WriteBits(pfn, uint64(0xffffffffffffffff))
}

func (f *ProcPageIdleBitmapFile) WriteBits(pfn uint64, bits uint64) error {
	pfnFileOffset := int64(pfn) / 64 * 8
	if _, err := f.osFile.Seek(pfnFileOffset, io.SeekStart); err != nil {
		return err
	}

	writeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(writeBuf, bits)
	n, err := f.osFile.Write(writeBuf)
	if err != nil {
		return err
	}
	if n != 8 {
		return fmt.Errorf("wrote %d instead of 8 bytes", n)
	}
	return nil
}

func (f *ProcPageIdleBitmapFile) ReadBits(pfn uint64) (uint64, error) {
	pfnFileOffset := int64(pfn) / 64 * 8
	if f.readCache != nil {
		if bits, ok := f.readCache[pfnFileOffset]; ok {
			return bits, nil
		}
	}
	if _, err := f.osFile.Seek(pfnFileOffset, io.SeekStart); err != nil {
		return 0, err
	}

	readBufSize := 8 * (f.readahead + 1)
	readBuf := make([]byte, readBufSize)
	n, err := io.ReadAtLeast(f.osFile, readBuf, readBufSize)
	if err != nil {
		return 0, err
	}
	if n != readBufSize {
		return 0, fmt.Errorf("read %d instead of expected %d bytes", n, readBufSize)
	}
	bits := binary.LittleEndian.Uint64(readBuf[:8])
	if f.readCache != nil {
		f.readCache[pfnFileOffset] = bits
		readBuf = readBuf[8:]
		for len(readBuf) > 0 {
			pfnFileOffset += 8
			bitsAhead := binary.LittleEndian.Uint64(readBuf[:8])
			f.readCache[pfnFileOffset] = bitsAhead
			readBuf = readBuf[8:]
		}
	}

	return bits, nil
}

func (f *ProcPageIdleBitmapFile) GetIdle(pfn uint64) (bool, error) {
	pfnBitOffset := pfn % 64
	bits, err := f.ReadBits(pfn)
	if err != nil {
		return false, err
	}
	pfnBitMask := (uint64(0x1) << pfnBitOffset)
	return (bits & pfnBitMask) != 0, nil
}

// ProcPagemapOpen returns opened pagemap file for a process
func ProcPagemapOpen(pid int) (*ProcPagemapFile, error) {
	path := "/proc/" + strconv.Itoa(pid) + "/pagemap"
	osFile, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	// The magic default readahead (63 pages in addition to the
	// page that is requested, resulting in 64 pages in total) is
	// based on a performance test on a vm. 1k buffer (16 B/page *
	// 64 pages) performed better than 512B or 4k).
	defaultReadahead := 63
	return &ProcPagemapFile{osFile, defaultReadahead, 0}, nil
}

func (f *ProcPagemapFile) Close() {
	if f.osFile != nil {
		f.osFile.Close()
		f.osFile = nil
	}
}

func (f *ProcPagemapFile) SetReadahead(pages int) {
	f.readahead = pages
}

// ForEachPage calls handlePage with pagemap bytes and page's address for
// every matching page in the address range.
//
// Parameters:
//   - addressRanges includes the address ranges from which pages
//     are searched from.
//   - pageAttributes defines attributes that found pages must or must
//     not have. Value 0 matches all pages.
//   - handlePage(pagemapBits, pageAddr) is called for
//     matching pages. It returns an integer:
//     0 (continue): ForEachPage continues reading the next page attributes.
//     -1 (break):   ForEachPage returns immediately.
//     n > 0 (skip): ForEachPage will skip reading next n pages.
func (f *ProcPagemapFile) ForEachPage(addressRanges []AddrRange, pageAttributes uint64, handlePage func(uint64, uint64) int) error {
	// Filter pages based on pagemap bits without calling handlePage.
	pageMustBePresent := (pageAttributes&PMPresentSet == PMPresentSet)
	pageMustNotBePresent := (pageAttributes&PMPresentCleared == PMPresentCleared)
	pageMustBeExclusive := (pageAttributes&PMExclusiveSet == PMExclusiveSet)
	pageMustNotBeExclusive := (pageAttributes&PMExclusiveCleared == PMExclusiveCleared)
	pageMustBeDirty := (pageAttributes&PMDirtySet == PMDirtySet)
	pageMustNotBeDirty := (pageAttributes&PMDirtyCleared == PMDirtyCleared)
	pagemapBitCheck := pageMustBePresent || pageMustNotBePresent ||
		pageMustBeExclusive || pageMustNotBeExclusive ||
		pageMustBeDirty || pageMustNotBeDirty
	for _, addressRange := range addressRanges {
		pagemapOffset := int64(addressRange.addr / constUPagesize * 8)
		// read /proc/pid/pagemap in the chunks of len(readBuf).
		// The length of readBuf must be divisible by 16.
		// Too short a readBuf slows down the execution due to
		// many read()'s.
		// Too long a readBuf makes the syscall return slowly.
		readBuf := make([]byte, 16*(1+f.readahead))
		readData := readBuf[0:0] // valid data in readBuf
		for pageIndex := uint64(0); pageIndex < addressRange.length; pageIndex++ {
			if len(readData) == 0 && f.read(&readData, &readBuf, &pagemapOffset, &addressRange, pageIndex) == 0 {
				break
			}
			bytes := readData[:8]
			readData = readData[8:]
			pagemapBits := binary.LittleEndian.Uint64(bytes)
			if pagemapBitCheck && !pagemapBitsSatisfied(pagemapBits,
				pageMustBePresent, pageMustNotBePresent,
				pageMustBeExclusive, pageMustNotBeExclusive,
				pageMustBeDirty, pageMustNotBeDirty) {
				continue
			}

			n := handlePage(pagemapBits, addressRange.addr+pageIndex*constUPagesize)
			switch {
			case n == 0:
				continue
			case n == -1:
				return nil
			case n > 0:
				// Skip next n pages
				pageIndex += uint64(n)
				pagemapOffset += int64(n * 8)
				// Consume read buffer
				if len(readData) < n*8 {
					readData = readData[n*8:]
				} else {
					readData = readData[0:0]
				}
			default:
				return fmt.Errorf("page handler callback returned invalid value: %d", n)
			}
		}
	}
	return nil
}

func (f *ProcPagemapFile) read(readData *[]byte, readBuf *[]byte, pagemapOffset *int64, addressRange *AddrRange, pageIndex uint64) int {
	if len(*readData) > 0 {
		return 0
	}
	// Seek if not already in the correct position.
	if f.pos != *pagemapOffset {
		_, err := f.osFile.Seek(*pagemapOffset, io.SeekStart)
		if err != nil {
			// Maybe there was a race condition and the maps changed?
			return 0
		}
		f.pos = *pagemapOffset
	}

	// Read from the correct position.
	unreadByteCount := 8 * int(addressRange.length-pageIndex)
	fillBufUpTo := cap(*readBuf)
	if fillBufUpTo > unreadByteCount {
		fillBufUpTo = unreadByteCount
	}
	nbytes, err := io.ReadAtLeast(f.osFile, *readBuf, fillBufUpTo)
	if err != nil {
		// cannot read address range
		return 0
	}
	f.pos += int64(nbytes)
	*pagemapOffset += int64(nbytes)
	*readData = (*readBuf)[:fillBufUpTo]
	return nbytes
}

func pagemapBitSatisfied(pagemapBits uint64, bit uint64, mustBeTrue, mustBeFalse bool) bool {
	if mustBeTrue || mustBeFalse {
		flag := (pagemapBits&bit == bit)
		if (mustBeTrue && !flag) ||
			(mustBeFalse && flag) {
			return false
		}
	}
	return true
}

func pagemapBitsSatisfied(pagemapBits uint64,
	pageMustBePresent, pageMustNotBePresent,
	pageMustBeExclusive, pageMustNotBeExclusive,
	pageMustBeDirty, pageMustNotBeDirty bool) bool {
	if !pagemapBitSatisfied(pagemapBits, PM_PRESENT, pageMustBePresent, pageMustNotBePresent) {
		return false
	}
	if !pagemapBitSatisfied(pagemapBits, PM_MMAP_EXCLUSIVE, pageMustBeExclusive, pageMustNotBeExclusive) {
		return false
	}
	if !pagemapBitSatisfied(pagemapBits, PM_SOFT_DIRTY, pageMustBeDirty, pageMustNotBeDirty) {
		return false
	}
	return true
}

// procNumaMaps helps parsing /proc/PID/numa_maps
// [root@n4c16-fedora-containerd fedora]# grep N[123]= /proc/44598/numa_maps                           // 00400000 default file=/usr/bin/meme mapped=128 mapmax=4 active=0 N1=113 N2=15 kernelpagesize_kB=4
// 004c6000 default file=/usr/bin/meme mapped=50 mapmax=4 active=0 N1=14 N2=36 kernelpagesize_kB=4
// 00578000 default file=/usr/bin/meme anon=6 dirty=6 mapped=8 mapmax=4 active=0 N0=6 N2=2 kernelpagesize_kB=4
// c000000000 default anon=179344 dirty=179344 active=0 N0=1942 N1=398 N2=916 N3=131171 N4=44917 kernelpagesize_kB=4
// 7fc1ba507000 default anon=223 dirty=223 active=0 N0=181 N1=2 N2=30 N3=10 kernelpagesize_kB=4
// 7fc20100e000 default anon=68 dirty=68 active=0 N0=53 N1=15 kernelpagesize_kB=4
// 7ffec41fe000 default stack anon=4 dirty=4 active=0 N0=3 N1=1 kernelpagesize_kB=4
func procNumaMaps(pid int, cb func(addr uint64, nodePagecount map[int]int64, pagesize int64, attrs map[string]string)) error {
	sPid := strconv.Itoa(pid)
	path := "/proc/" + sPid + "/numa_maps"
	data, err := procRead(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		addr, err := strconv.ParseUint(fields[0], 16, 64)
		if err != nil {
			continue
		}
		attrs := make(map[string]string)
		nodePagecount := make(map[int]int64)
		pagesize := int64(0)
		for _, field := range fields[1:] {
			keyValue := strings.Split(field, "=")
			if len(keyValue) == 2 && len(keyValue[0]) > 0 {
				attrs[keyValue[0]] = keyValue[1]
				if keyValue[0][0] == 'N' {
					node, err := strconv.Atoi(keyValue[0][1:])
					if err != nil {
						continue
					}
					pages, err := strconv.Atoi(keyValue[1])
					nodePagecount[node] = int64(pages)
				}
				if keyValue[0] == "kernelpagesize_kB" {
					pagesizeKB, err := strconv.Atoi(keyValue[1])
					if err != nil {
						continue
					}
					pagesize = int64(pagesizeKB) * 1024
				}
			}
		}
		if len(attrs) > 0 {
			cb(addr, nodePagecount, pagesize, attrs)
		}
	}
	return nil
}

// procMaps returns address ranges of a process
func procMaps(pid int) ([]AddrRange, error) {
	pageCanBeInAnonymous := true
	pageCanBeInHeap := true
	pageCanBeInFile := false // TODO: should be configurable

	addressRanges := make([]AddrRange, 0)
	sPid := strconv.Itoa(pid)

	// Read /proc/pid/numa_maps
	numaMapsPath := "/proc/" + sPid + "/numa_maps"
	numaMapsBytes, err := os.ReadFile(numaMapsPath)
	if err != nil {
		return nil, err
	}
	numaMapsLines := strings.Split(string(numaMapsBytes), "\n")

	// Read /proc/pid/maps
	mapsPath := "/proc/" + sPid + "/maps"
	mapsBytes, err := os.ReadFile(mapsPath)
	if err != nil {
		return nil, err
	}
	mapsLines := strings.Split(string(mapsBytes), "\n")

	allAddressRanges := make(map[uint64]AddrRange, len(numaMapsLines))
	for _, mapLine := range mapsLines {
		// Parse start and end addresses. Example of /proc/pid/maps lines:
		// 55d74cf13000-55d74cf14000 rw-p 00003000 fe:03 1194719   /usr/bin/python3.8
		// 55d74e76d000-55d74e968000 rw-p 00000000 00:00 0         [heap]
		// 7f3bcfe69000-7f3c4fe6a000 rw-p 00000000 00:00 0
		dashIndex := strings.Index(mapLine, "-")
		spaceIndex := strings.Index(mapLine, " ")
		if dashIndex > 0 && spaceIndex > dashIndex {
			startAddr, err := strconv.ParseUint(mapLine[0:dashIndex], 16, 64)
			if err != nil {
				continue
			}
			endAddr, err := strconv.ParseUint(mapLine[dashIndex+1:spaceIndex], 16, 64)
			if err != nil || endAddr < startAddr {
				continue
			}
			rangeLength := endAddr - startAddr
			allAddressRanges[startAddr] = AddrRange{startAddr, rangeLength / constUPagesize}
		}
	}

	for _, line := range numaMapsLines {
		// Example of /proc/pid/numa_maps:
		// 55d74cf13000 default file=/usr/bin/python3.8 anon=1 dirty=1 active=0 N0=1 kernelpagesize_kB=4
		// 55d74e76d000 default heap anon=471 dirty=471 active=0 N0=471 kernelpagesize_kB=4
		// 7f3bcfe69000 default anon=524289 dirty=524289 active=0 N0=257944 N1=266345 kernelpagesize_kB=4
		// // next from: shmget(IPC_PRIVATE, 1000000, IPC_CREAT|IPC_EXCL|SHM_HUGETLB|0600) = 10
		// 7f0ca5000000 default file=/SYSV00000000\040(deleted) huge dirty=1 N1=1 kernelpagesize_kB=2048
		// // next from: shmget(IPC_PRIVATE, 10000000, IPC_CREAT|SHM_HUGETLB|0600) = 11
		// 7f0ca4600000 default file=/SYSV00000000\040(deleted) huge dirty=5 N1=5 kernelpagesize_kB=2048

		tokens := strings.Split(line, " ")
		if len(tokens) < 3 {
			continue
		}
		attrs := strings.Join(tokens[2:], " ")

		// Filter out lines which don't have "anonymous", since we are not
		// interested in file-mapped or shared pages. Save the interesting ranges.
		// TODO: consider dropping the "heap" requirement. There are often ranges
		// in the file which don't have any attributes indicating the memory
		// location.
		// TODO: rather than filtering here, consider parsing properties
		// (like on which nodes pages in the range are located, heap/dirty/active...)
		// to AddrRange{} structs so that they can be filtered later on
		// for instance ar.IsDirty().OnNodes(2, 3)
		if !(pageCanBeInHeap && strings.Contains(attrs, "heap") ||
			pageCanBeInAnonymous && strings.Contains(attrs, "anon=") ||
			pageCanBeInFile && strings.Contains(attrs, "file=")) {
			continue
		}
		startAddr, err := strconv.ParseUint(tokens[0], 16, 64)
		if err != nil {
			continue
		}
		if ar, ok := allAddressRanges[startAddr]; ok {
			addressRanges = append(addressRanges, ar)
		}
	}
	return addressRanges, nil
}

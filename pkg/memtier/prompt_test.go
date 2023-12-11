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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func FuzzPrompt(f *testing.F) {
	fuzzPromptCreateEnv := os.Getenv("FUZZ_PROMPT_CREATE")
	fuzzPromptCreate := true
	if len(fuzzPromptCreateEnv) > 0 {
		if strings.Contains("nN0", fuzzPromptCreateEnv) {
			fuzzPromptCreate = false
		} else if !strings.Contains("yY1", fuzzPromptCreateEnv) {
			f.Errorf("invalid value in environment variable FUZZ_PROMPT_CREATE, expected one of yY1nN0")
		}
	}
	pidwatcherCommonArgs := " -listener log -config-dump -poll -start -stop -dump"
	trackerCommonArgs := " -config-dump -start -stop -dump"
	policyCommonArgs := " -config-dump -start -stop -dump"
	routinesCommonArgs := " -ls -use 0 -config-dump -start -stop -dump"
	moverCommonArgs := " -config-dump -start -tasks"
	pagesCommonArgs := fmt.Sprintf(" -pid %d ", os.Getpid())
	testcases := []string{
		"help",
		"nop",
		"pages -attrs Exclusive,Dirty,InHeap" + pagesCommonArgs,
		"pages -node 0" + pagesCommonArgs,
		"pages -pi 123456" + pagesCommonArgs,
		"pages -si 123456" + pagesCommonArgs,
		"pages -pr=5 -pm=5" + pagesCommonArgs,
		"pages -ranges=c0000000000" + pagesCommonArgs,
		"pidwatcher -ls",
		"pidwatcher -create pidlist -config {\"Pids\":[42,4242]}" + pidwatcherCommonArgs,
		"pidwatcher -create cgroups -config {\"IntervalMs\":10000,\"Cgroups\":[\"/sys/fs/cgroup/memtierd-test\"]}" + pidwatcherCommonArgs,
		"pidwatcher -create proc -config {\"IntervalMs\":10000}" + pidwatcherCommonArgs,
		"pidwatcher -create pidlist -config {\"Pids\":[42,4242]}" + pidwatcherCommonArgs,
		"pidwatcher -create filter -config {}" + pidwatcherCommonArgs,
		"policy -ls",
		"policy -create age -config {\"IntervalMs\":10000}" + policyCommonArgs,
		"routines -create statactions -config {\"IntervalMs\":10000}" + routinesCommonArgs,
		"tracker -ls",
		"tracker -create damon" + trackerCommonArgs,
		"tracker -create idlepage" + trackerCommonArgs,
		"tracker -create softdirty" + trackerCommonArgs,
		"mover -config {\"IntervalMs\":50,\"Bandwidth\":1000} -pages-to 0" + moverCommonArgs,
		"mover -config {\"IntervalMs\":500,\"Bandwidth\":1} -start -pages-to 1 -wait" + moverCommonArgs,
		"mover -pause -start -stop -pages-to 0 -tasks" + moverCommonArgs,
		"stats",
		"stats -f csv",
		"stats -f txt",
		"stats -le 10",
		"stats -lm 10",
		"stats -t events,move_pages",
		"q",
	}

	for _, tc := range testcases {
		f.Add(tc)
	}

	var promptOutBuf bytes.Buffer
	promptOut := bufio.NewReadWriter(
		bufio.NewReader(&promptOutBuf),
		bufio.NewWriter(&promptOutBuf))
	prompt := NewPrompt("memtierd-fuzzed> ", bufio.NewReader(strings.NewReader("")), promptOut.Writer)

	f.Fuzz(func(t *testing.T, input string) {
		if !fuzzPromptCreate && strings.Contains(input, "-create") {
			return
		}
		if strings.Contains(input, "|") {
			// Do not fuzz inputs with pipes, as it would
			// execute fuzzed strings in shell.
			return
		}
		t.Logf("input: %q\n", input)
		prompt.RunCmdString(input)
		time.Sleep(time.Millisecond)
		out := []byte{}
		if _, err := promptOut.Read(out); err == nil {
			t.Logf("---response-begin---\n%s---response-end---\n", out)
		} else {
			t.Errorf("error reading output of input %q: %s", input, err)
		}

	})
}

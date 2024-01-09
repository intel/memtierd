#!/bin/bash

# Make sure the system has IDLEPAGE support enabled
# Note: IDLEPAGE is enabled on ubuntu-22.04 by default
# shellcheck disable=SC2154
vm-command "[ -f /sys/kernel/mm/page_idle/bitmap ]" || {
    if [[ "$distro" != "debian-sid" && "$distro" != "ubuntu-22.04" ]]; then
        error "idlepage e2e test is implemented only for distro=debian-sid or distro=ubuntu-22.04"
    fi

    if [ "$distro" == "debian-sid" ]; then
        damon-idlepage-setup
    fi

    vm-command "[ -f /sys/kernel/mm/page_idle/bitmap ]" || error "failed to setup idlepage"
}

memtierd-setup

# Create swap
zram-install
zram-swap off
zram-swap 2G

# Test that 700M+ is swapped out from a process that has 1G of memory
# reads actively 300M
MEME_CGROUP=e2e-meme
MEME_BS=1G MEME_BRC=1 MEME_BRS=300M memtierd-meme-start

# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: heat
  config: |
    intervalms: 4000
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/${MEME_CGROUP}
    heatnumas:
      0: [-1]
    heatmap:
      heatmax: 0.01
      heatretention: 0
      heatclasses: 5
    tracker:
      name: idlepage
      config: |
        pagesinregion: 512
        maxcountperregion: 0
        scanintervalms: 500
        regionsupdatems: 0
        pagemapreadahead: 0
        kpageflagsreadahead: 0
        bitmapreadahead: 0
    mover:
      intervalms: 20
      bandwidth: 100
"
memtierd-start

sleep 4

echo "waiting 700M+ to be paged out..."
memtierd-match-pageout "0\.7[0-9][0-9]" 5 6

echo "check swap status: correct pages have been paged out."
memtierd-command "swap -pid $MEME_PID -status"
grep " 7[0-9][0-9] " <<<"$COMMAND_OUTPUT" || {
    error "expected 7XX MB of swapped out"
}

echo "stop meme, expect all memory to be paged out"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pageout "1\.[0-2]" 5 6

echo "continue meme, expect 300 MB to be swapped in by OS"
vm-command "kill -CONT ${MEME_PID}"
sleep 4
memtierd-command "swap -pid $MEME_PID -status"
grep " 7[0-9][0-9] " <<<"$COMMAND_OUTPUT" || {
    error "expected 7XX MB of swapped out"
}

echo "stop meme again, verify that all memory gets paged out again"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pageout "1\.[2-5]" 6 6

memtierd-stop
memtierd-meme-stop

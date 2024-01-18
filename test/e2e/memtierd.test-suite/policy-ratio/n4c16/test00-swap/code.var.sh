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

compare-ratios() {
    local actual_ratio
    local expected_ratio
    local diff_ratio
    actual_ratio="$(bc -l <<<"$1")"
    expected_ratio="$(bc -l <<<"$2")"
    diff_ratio="$(echo "$expected_ratio - $actual_ratio" | bc -l)"
    echo "expected ratio percentage: $expected_ratio, got: $actual_ratio, diff: $diff_ratio"
    if (($(echo "$diff_ratio <= 5 && $diff_ratio >= -5" | bc -l))); then
        return 0 # true
    else
        return 1 # false
    fi
}

# shellcheck disable=SC2034
match-pid-swapout-ratio() {
    local pid=$1
    local expected_ratio=$2
    local round_counter_max=$3
    local round_delay=$4
    round_number=0
    while ! (
        memtierd-command "swap -pid $pid -status"
        actual_ratio="$(echo "$COMMAND_OUTPUT" | grep -oP '\(\K[0-9.]+ %\)' | sed 's/[()%]//g' | head -n 1)"
        compare-ratios "${actual_ratio}" "${expected_ratio}"
    ); do
        echo "the SWAPOUT RATIO is not within 5% of the expected ratio yet"
        next-round round_number "${round_counter_max}" "${round_delay}" || {
            error "timeout: memtierd did not expected amount of memory"
        }
    done
}

match-system-swapout() {
    local pid=$1
    local expected_ratio=$2
    echo "# verifying that process $pid swapout percentage is $expected_ratio"
    vm-command "awk '/VmSwap/{swap=\$2}/VmRSS/{rss=\$2}END{print 100*swap/(swap+rss)}' < /proc/$pid/status"
    if ! compare-ratios "$COMMAND_OUTPUT" "$expected_ratio"; then
        vm-command "grep Vm /proc/$pid/status"
        error "match-system-swapout: pid $pid not swapped out"
    fi
}

# Start meme process which has 1G of memory and reads actively 300M
MEME_BS=1G MEME_BRC=1 MEME_BRS=50M memtierd-meme-start

echo -e "\n=== scenario 1: test swapping out memory with policy-ratio and tracker-softdirty ===\n"
MEMTIERD_YAML="
policy:
  name: ratio
  config: |
    intervalms: 4000
    ratio: 0.3
    pidwatcher:
      name: pidlist
      config: |
        pids:
          - $MEME_PID
    tracker:
      name: softdirty
      config: |
        pagesinregion: 256
        maxcountperregion: 0
        scanintervalms: 4000
        regionsupdatems: 0
        skippageprob: 0
        pagemapreadahead: 0
    mover:
      intervalms: 20
      bandwidth: 100
"
memtierd-start

sleep 5
match-pid-swapout-ratio "${MEME_PID}" 30 5 5
match-system-swapout "${MEME_PID}" 30

memtierd-stop
memtierd-meme-stop

# Start meme process which has 1G of memory and reads actively 300M
MEME_BS=1G MEME_BRC=1 MEME_BRS=50M memtierd-meme-start

echo -e "\n=== scenario 2: test swapping out memory with policy-ratio and tracker-idlepage ===\n"
# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: ratio
  config: |
    intervalms: 4000
    ratio: 0.3
    pidwatcher:
      name: pidlist
      config: |
        pids:
          - $MEME_PID
    tracker:
      name: idlepage
      config: |
        pagesinregion: 512
        maxcountperregion: 1
        scanintervalms: 4000
    mover:
      intervalms: 20
      bandwidth: 100
"
memtierd-start

sleep 5
match-pid-swapout-ratio "${MEME_PID}" 30 5 5

memtierd-stop
memtierd-meme-stop

echo -e "\n=== scenario 3: test swapping out all memory of old and new processes in a cgroup without tracking memory accesses ===\n"
export MEME_CGROUP=e2e-meme-swapall
MEME_BS=100M MEME_BRC=0 MEME_BWC=0 memtierd-meme-start
MEME1_PID=$MEME_PID
MEMTIERD_YAML="
policy:
  name: ratio
  config: |
    intervalms: 1000
    ratio: 1.0
    ratiotargets: [-1]
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/$MEME_CGROUP
    tracker:
      name: finder
      config: |
        regionsupdatems: 500
    mover:
      intervalms: 20
      bandwidth: 100
"
memtierd-start
sleep 5
match-system-swapout "${MEME1_PID}" 100

echo "# launching second meme into the same cgroup"
echo "# expect the finder tracker to report its memory"
echo "# and the ratio policy to swap it out"
MEME_BS=200M MEME_BRC=0 MEME_BWC=0 memtierd-meme-start
MEME2_PID=$MEME_PID
sleep 5
match-system-swapout "${MEME1_PID}" 100
match-system-swapout "${MEME2_PID}" 100

memtierd-stop
memtierd-meme-stop
unset MEME_CGROUP

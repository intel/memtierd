#!/bin/bash

memtierd-setup

# Create swap
zram-install
zram-swap off
zram-swap 2G

# Test meme process which has 1G of memory and reads actively 300M
# 700M+ should be moved to idlenumas and 300M should be moved to activenumas
MEME_BS=1G MEME_BWC=1 MEME_BWS=300M memtierd-meme-start

# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: age
  config: |
    intervalms: 4000
    pidwatcher:
      name: pidlist
      config: |
        pids:
          - $MEME_PID
    swapoutms: 5000
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
grep " 7[0-9][0-9] " <<<$"$COMMAND_OUTPUT" || {
    error "expected 7XX MB of swapped out"
}

echo "stop meme again, verify that all memory gets paged out again"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pageout "1\.[2-5]" 5 6

memtierd-stop
memtierd-meme-stop

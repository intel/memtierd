#!/bin/bash

memtierd-setup

# Test meme process which has 1G of memory and writes actively 300M
# 700M+ should be moved to idlenumas and 300M should be moved to activenumas
MEME_CGROUP=e2e-meme
MEME_BS=1G MEME_BWC=1 MEME_BWS=300M MEME_MEMS=0 memtierd-meme-start
sleep 2

# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: age
  config: |
    intervalms: 5000
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/${MEME_CGROUP}
    idledurationms: 8000
    idlenumas: [3]
    activedurationms: 6000
    activenumas: [1]
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
      bandwidth: 200
"
memtierd-start

sleep 4

echo "waiting 200M+ (active) to be moved to node 1 and 700M+ (idle) to be moved to node 3"
memtierd-match-pagemoving "1\:0.2[0-9][0-9]" 5 6
memtierd-match-pagemoving "3\:0\.7[0-9][0-9]" 5 6

echo "stop meme, expect all memory to be moved to node 3"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pagemoving "3\:1\.[0-2]" 5 6

echo "continue meme, expect ~300 MB to be moved to node 1"
vm-command "kill -CONT ${MEME_PID}"
memtierd-match-pagemoving "1\:0.5[0-9][0-9]" 5 6

echo "stop meme again, expect ~300 MB to be moved to node 3"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pagemoving "3\:1\.[2-4]" 5 6

memtierd-stop
memtierd-meme-stop

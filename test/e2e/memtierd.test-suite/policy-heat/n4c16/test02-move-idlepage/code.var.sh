#!/bin/bash

memtierd-setup

# Start a process that has 1G of memory and reads actively 260M.
MEME_CGROUP=e2e-meme
MEME_BS=1G MEME_BRC=1 MEME_BRS=260M MEME_MEMS=0 memtierd-meme-start

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
      0: [3]
      1: [1]
    heatmap:
      heatmax: 0.01
      heatretention: 0
      heatclasses: 2
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

echo "waiting ~260 MB (hot) to be moved to node 1 and ~700 MB (cold) to be moved to node 3"
memtierd-match-pagemoving "1\:0.2[0-9][0-9]\;3\:0\.7[0-9][0-9]" 5 4

echo "stop meme, expect all memory to be moved to node 3"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pagemoving "1\:0.2[0-9][0-9]\;3\:1\.[0-2]" 3 4

echo "continue meme, expect ~260 MB to be moved back to node 1 again"
vm-command "kill -CONT ${MEME_PID}"
memtierd-match-pagemoving "1\:0.5[0-9][0-9]\;3\:1\.[0-2]" 3 4

echo "stop meme again, expect ~260 MB to be moved to node 3"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pagemoving "1\:0.5[0-9][0-9]\;3\:1\.[2-4]" 3 4

memtierd-stop
memtierd-meme-stop

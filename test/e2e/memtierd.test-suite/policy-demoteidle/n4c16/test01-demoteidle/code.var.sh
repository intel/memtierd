#!/bin/bash

# Make sure the system has IDLEPAGE support enabled
# shellcheck disable=SC2154
vm-command "[ -f /sys/kernel/mm/page_idle/bitmap ]" || {
    error "idlepage support required for demote-idle policy"
}

# Disable transparent huge pages: idle page tracking operates at
# 4K granularity; with THPs any sub-page access marks the entire
# compound page active, causing Go GC to hide truly idle pages.
vm-command "echo never > /sys/kernel/mm/transparent_hugepage/enabled"

# Install numactl so we can migrate all meme memory to node 0
# regardless of where Go runtime initially allocated it.
vm-command "which migratepages >/dev/null 2>&1 || apt-get install -y -qq numactl"

memtierd-setup

# Test meme process which has 1G of memory and reads actively 300M.
# ~700M idle should be demoted to node 3.
MEME_CGROUP=e2e-meme
MEME_BS=1G MEME_BRC=1 MEME_BRS=300M MEME_MEMS=0 memtierd-meme-start

# Ensure all of meme's memory starts on node 0. memtierd-meme-start
# sets cpuset.mems after meme has already allocated, so Go runtime
# may have spread pages across nodes.
vm-command "migratepages ${MEME_PID} 1,2,3 0"
sleep 2

# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: demote-idle
  config: |
    intervalms: 4000
    idlenumas: [3]
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/${MEME_CGROUP}
    mover:
      intervalms: 20
      bandwidth: 10000
"
memtierd-start
MEMTIERD_START_TIME=$(date +%s)

sleep 4

echo "waiting 700M+ (idle) to be moved to node 3"
memtierd-match-pagemoving "3\:0\.7[0-9][0-9]" 12 8
PHASE1_DONE_TIME=$(date +%s)
PHASE1_ELAPSED=$(( PHASE1_DONE_TIME - MEMTIERD_START_TIME ))
echo "TIMING: phase 1 (idle demotion) completed in ${PHASE1_ELAPSED}s"

echo "stop meme, expect remaining ~300M to be moved to node 3 too"
vm-command "kill -STOP ${MEME_PID}"
memtierd-match-pagemoving "3\:1\.[0-2]" 8 8
PHASE2_DONE_TIME=$(date +%s)
PHASE2_ELAPSED=$(( PHASE2_DONE_TIME - MEMTIERD_START_TIME ))
echo "TIMING: phase 2 (full demotion) completed in ${PHASE2_ELAPSED}s"

memtierd-stop
memtierd-meme-stop

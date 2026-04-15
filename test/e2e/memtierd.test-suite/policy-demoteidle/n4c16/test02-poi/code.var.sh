#!/bin/bash

# Test demote-idle policy with PagesOfInterestDir, verifying:
# 1. flock-based locking between external writer and memtierd
# 2. PID.vab address range precision (only specified ranges get demoted)
# 3. Incremental POI updates

# --- Helper functions for external POI writer ---
# These demonstrate the locking protocol for external POI generators.

# poi-lock: acquire exclusive lock on the POI directory.
# Creates the lock file if it does not exist. Holds LOCK_EX until
# poi-unlock is called. While held, memtierd skips the scan round.
# Uses flock -o (--close) so the child process does not inherit the
# lock fd — otherwise killing flock leaves an orphan holding LOCK_EX.
poi-lock() {
    vm-command "nohup flock -o -x ${POI_DIR}/lock -c 'sleep 3600' </dev/null >/dev/null 2>&1 & echo \$!"
    POI_LOCK_PID=${COMMAND_OUTPUT##*$'\n'}
    sleep 1
}

# poi-unlock: release the exclusive lock by terminating the holder.
# Verifies via fuser that the lock is actually released.
poi-unlock() {
    vm-command "kill ${POI_LOCK_PID} 2>/dev/null; sleep 0.5; fuser ${POI_DIR}/lock 2>/dev/null && echo 'WARNING: lock still held' || echo 'lock released ok'"
    sleep 1
}

# --- Standard test setup ---

# shellcheck disable=SC2154
vm-command "[ -f /sys/kernel/mm/page_idle/bitmap ]" || {
    error "idlepage support required for demote-idle policy"
}

vm-command "echo never > /sys/kernel/mm/transparent_hugepage/enabled"
vm-command "which migratepages >/dev/null 2>&1 || apt-get install -y -qq numactl"

memtierd-setup

# Start meme: 1G total, actively reading 300M from offset 0.
MEME_CGROUP=e2e-meme
MEME_BS=1G MEME_BRC=1 MEME_BRS=300M MEME_MEMS=0 memtierd-meme-start

# Move all memory to node 0.
vm-command "migratepages ${MEME_PID} 1,2,3 0"
sleep 2

# Parse meme array address and size.
vm-command "cat meme0.output.txt"
ARRAY_START=$(awk '/array:/{split($2,a,"-"); print a[1]}' <<<"$COMMAND_OUTPUT")
ARRAY_BYTES=$(awk '/array:/{gsub(/[()]/,"",$3); print $3}' <<<"$COMMAND_OUTPUT")
echo "meme array: start=0x${ARRAY_START} bytes=${ARRAY_BYTES}"

# Meme layout (BRS=300M, BRO=0):
#   [0, 300M)   = active (being read)
#   [300M, 1G)  = idle
ACTIVE_BYTES=314572800
IDLE_OFFSET=${ACTIVE_BYTES}
# Phase 1: 200M of idle range
PHASE1_SIZE=209715200
# Phase 2: remaining idle range
PHASE2_OFFSET=$(( IDLE_OFFSET + PHASE1_SIZE ))
PHASE2_SIZE=$(( ARRAY_BYTES - PHASE2_OFFSET ))
echo "phase 1 range: offset=${IDLE_OFFSET} size=${PHASE1_SIZE} (200M)"
echo "phase 2 range: offset=${PHASE2_OFFSET} size=${PHASE2_SIZE}"

# --- Start with EMPTY POI directory (no lock file, no .vab) ---
POI_DIR=/tmp/poi-test02
vm-command "rm -rf ${POI_DIR} && mkdir -p ${POI_DIR}"

# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: demote-idle
  config: |
    intervalms: 4000
    idlenumas: [3]
    pagesofinterestdir: ${POI_DIR}
    mover:
      intervalms: 20
      bandwidth: 10000
"
memtierd-start
MEMTIERD_START_TIME=$(date +%s)

# Verify: empty dir (no lock file) → no pages move.
sleep 8
vm-command "awk '{for(i=1;i<=NF;i++){if(\$i~/^N3=/){split(\$i,a,\"=\");s+=a[2]}}} END{print s+0}' /proc/${MEME_PID}/numa_maps"
N3_PAGES=${COMMAND_OUTPUT##*$'\n'}
N3_MB=$(( N3_PAGES * 4096 / 1048576 ))
echo "empty dir check: node3=${N3_MB}MB (${N3_PAGES} pages)"
if (( N3_MB > 20 )); then
    error "expected no movement with empty POI dir, got ${N3_MB}MB on node 3"
fi

# --- Phase 1: lock, write small idle range (200M), unlock ---
echo "=== Phase 1: adding 200M idle range ==="
poi-lock

# Write .vab with 200M of idle range starting after the active region.
vm-command "python3 -c \"
import struct
start = 0x${ARRAY_START} + ${IDLE_OFFSET}
end = start + ${PHASE1_SIZE}
with open('${POI_DIR}/${MEME_PID}.vab', 'wb') as f:
    f.write(struct.pack('<QQ', start, end))
\""

# While lock is held, memtierd cannot read → nothing should move yet.
sleep 10
vm-command "awk '{for(i=1;i<=NF;i++){if(\$i~/^N3=/){split(\$i,a,\"=\");s+=a[2]}}} END{print s+0}' /proc/${MEME_PID}/numa_maps"
N3_PAGES=${COMMAND_OUTPUT##*$'\n'}
N3_MB=$(( N3_PAGES * 4096 / 1048576 ))
echo "lock held check: node3=${N3_MB}MB (${N3_PAGES} pages)"
if (( N3_MB > 20 )); then
    error "pages moved while lock was held: ${N3_MB}MB on node 3"
fi

poi-unlock

# Wait for memtierd: round 1 sets idle bits, round 2 detects + moves.
sleep 16
vm-command "awk '{for(i=1;i<=NF;i++){if(\$i~/^N3=/){split(\$i,a,\"=\");s+=a[2]}}} END{print s+0}' /proc/${MEME_PID}/numa_maps"
N3_PAGES=${COMMAND_OUTPUT##*$'\n'}
N3_MB=$(( N3_PAGES * 4096 / 1048576 ))
echo "phase 1 result: node3=${N3_MB}MB (${N3_PAGES} pages)"

# Should have moved ~200M — not more (upper bound verifies range precision).
PHASE1_DONE_TIME=$(date +%s)
PHASE1_ELAPSED=$(( PHASE1_DONE_TIME - MEMTIERD_START_TIME ))
echo "TIMING: phase 1 (200M idle demotion) completed in ${PHASE1_ELAPSED}s"
if (( N3_MB < 100 )); then
    error "phase 1: expected >=100MB on node 3, got ${N3_MB}MB"
fi
if (( N3_MB > 350 )); then
    error "phase 1: range precision violated, expected <=350MB but got ${N3_MB}MB on node 3"
fi

# --- Phase 2: lock, add remaining idle range, unlock ---
echo "=== Phase 2: adding remaining idle range ==="
poi-lock

# Rewrite .vab with the remaining idle range (not yet demoted).
vm-command "python3 -c \"
import struct
start = 0x${ARRAY_START} + ${PHASE2_OFFSET}
end = start + ${PHASE2_SIZE}
with open('${POI_DIR}/${MEME_PID}.vab', 'wb') as f:
    f.write(struct.pack('<QQ', start, end))
\""

poi-unlock

# Wait for 2 rounds: set idle bits then check + move.
sleep 16
vm-command "awk '{for(i=1;i<=NF;i++){if(\$i~/^N3=/){split(\$i,a,\"=\");s+=a[2]}}} END{print s+0}' /proc/${MEME_PID}/numa_maps"
N3_PAGES=${COMMAND_OUTPUT##*$'\n'}
N3_MB=$(( N3_PAGES * 4096 / 1048576 ))
echo "phase 2 result: node3=${N3_MB}MB (${N3_PAGES} pages)"

# Should have ~200M (phase 1) + ~500M (phase 2) = ~700M total on node 3.
PHASE2_DONE_TIME=$(date +%s)
PHASE2_ELAPSED=$(( PHASE2_DONE_TIME - MEMTIERD_START_TIME ))
echo "TIMING: phase 2 (full idle demotion) completed in ${PHASE2_ELAPSED}s"
if (( N3_MB < 550 )); then
    error "phase 2: expected >=550MB on node 3, got ${N3_MB}MB"
fi

memtierd-stop
memtierd-meme-stop
vm-command "rm -rf ${POI_DIR}"

memtierd-setup

# Create swap
zram-install
zram-swap off
zram-swap 2G

# In general, it takes around 5 seconds to finish moving out
match-moved() {
    local moved_regexp=$1
    local target_numa_node=$2
    local min_seconds=$3
    local max_seconds=$4
    round_number=0
    while ! (
        memtierd-command "stats -t move_pages -f csv | awk -F, \"{print \\\$6}\""
        grep ${moved_regexp} <<<$COMMAND_OUTPUT
    ); do
        echo "grep MOVED value to the target numa node ${target_numa_node} matching ${moved_regexp} not found"
        next-round round_number $max_seconds 1s || {
            error "timeout: memtierd did not expected amount of memory"
        }
    done
    if [[ "$round_number" -lt "$min_seconds" ]]; then
        error "too fast page moves, expected at least $min_seconds seconds, got $round_number"
    fi
    echo "pages moved in less than $((round_number + 1)) seconds that is in expected range [$min_seconds, $max_seconds]"
}

match-pid-pageout() {
    local pid=$1
    local pageout_regexp=$2
    local min_seconds=$3
    local max_seconds=$4
    round_number=0
    while ! (
        memtierd-command "swap -pid $pid -status | awk \"{print \\\$6}\""
        grep ${pageout_regexp} <<<$COMMAND_OUTPUT
    ); do
        echo "grep PAGEOUT value matching ${pageout_regexp} not found"
        next-round round_number $max_seconds 1s || {
            error "timeout: memtierd did not swap expected amount of memory"
        }
    done
    if [[ "$round_number" -lt "$min_seconds" ]]; then
        error "too fast swapping, expected at least $min_seconds seconds, got $round_number"
    fi
    echo "swapping out in less than $((round_number + 1)) seconds that is in expected range [$min_seconds, $max_seconds]"
}

mover_conf=(
    '-config-dump'
    '-config {"IntervalMs":1000,"Bandwidth":100000}'
    '-config {"IntervalMs":10,"Bandwidth":200}'
    '-config {"IntervalMs":20,"Bandwidth":500}'
)

mover_min_max_seconds=(
    "0 10"
    "0 1"
    "2 4"
    "1 2"
)

mover_swap_conf=(
    '-config {"IntervalMs":10,"Bandwidth":5}'
    '-config {"IntervalMs":20,"Bandwidth":10}'
    '-config {"IntervalMs":1000,"Bandwidth":10}'
)

mover_swap_min_max_seconds=(
    "4 7"
    "2 4"
    "2 6"
)

echo -e "\n=== scenario 1: test moving memories among numa nodes ===\n"

# Test that 700M+ is moved to numa node {0..3} from a process
MEME_BS=720M MEME_BWC=1 MEME_BWS=720M memtierd-meme-start

for target_numa_node in {0..3}; do
    MEMTIERD_YAML=""
    memtierd-start
    memtierd-command "pages -pid ${MEME_PID}"
    memtierd-command "mover ${mover_conf[$target_numa_node]} -pages-to ${target_numa_node}"
    echo "waiting 700M+ to be moved to ${target_numa_node}"
    match-moved "${target_numa_node}\:0\.7[0-9][0-9]" ${target_numa_node} ${mover_min_max_seconds[$target_numa_node]}
    memtierd-stop
done

memtierd-meme-stop

echo -e "\n=== scenario 2: test swapping out memories ===\n"

echo -e "\n=== scenario 2.1: test swapping out memories for one process with various mover configurations ===\n"

for i in {0..2}; do
    # Test that 20M+ is swapped out
    MEME_BS=50M MEME_BWC=1 MEME_BWS=22M memtierd-meme-start

    MEMTIERD_YAML=""
    memtierd-start

    # the 1st time swapping out with -mover option
    memtierd-command "mover ${mover_swap_conf[$i]}\nswap -out -pids ${MEME_PID} -mover\nmover -wait"
    match-pid-pageout ${MEME_PID} "[2-3][0-9]" ${mover_swap_min_max_seconds[$i]}

    # swapping in all the memory which have been swapped out above
    # thus, 0-9 MB will be swapped out after swapping in option
    memtierd-command "swap -in -pids ${MEME_PID}"
    match-pid-pageout ${MEME_PID} "[0-9]" 0 1

    # the 2nd time swapping out without -mover option
    memtierd-command "swap -out -pids ${MEME_PID}"
    match-pid-pageout ${MEME_PID} "[2-3][0-9]" 0 1

    memtierd-meme-stop
    memtierd-stop
done

echo -e "\n=== scenario 2.2: test swapping out memories for multiple processes ===\n"

# Start 3 meme processes, each should have 20M+ swapped out
for j in {0..2}; do
    MEME_BS=50M MEME_BWC=1 MEME_BWS=22M memtierd-meme-start
    PID_VAR="MEME${j}_PID"
    eval "${PID_VAR}=${MEME_PID}"
done
MEME_PIDS=("${MEME0_PID}" "${MEME1_PID}" "${MEME2_PID}")

MEMTIERD_YAML=""
memtierd-start

vm-command "mkdir -p /sys/fs/cgroup/e2e-meme; echo ${MEME2_PID} > /sys/fs/cgroup/e2e-meme/cgroup.procs"


# the 1st time swapping out with -mover option for the three meme processes.
# Test all methods to give PIDs to swap: -pid, -pids and -pid-cgroups.
memtierd-command "mover -config {\"IntervalMs\":10,\"Bandwidth\":10}\nswap -out -pids ${MEME0_PID} -pid ${MEME1_PID} -pid-cgroups /sys/fs/cgroup/e2e-meme -mover\nmover -wait"

# As mover handles the multiple pids' tasks one by one, thus, set the max_seconds four times greater and set the min_seconds as 0
for MEME_PID in "${MEME_PIDS[@]}"; do
    memtierd-command "swap -pid ${MEME_PID} -status"
    match-pid-pageout "${MEME_PID}" "[2-3][0-9]" 0 16
done

# swapping in all the memory which have been swapped out above for the three meme processes
memtierd-command "swap -in -pids ${MEME0_PID},${MEME1_PID},${MEME2_PID}"
for MEME_PID in "${MEME_PIDS[@]}"; do
    memtierd-command "swap -pid ${MEME_PID} -status"
    match-pid-pageout "${MEME_PID}" "[0-9]" 0 1
done

# the 2nd time swapping out without -mover option for the three meme processes
memtierd-command "swap -out -pids ${MEME0_PID},${MEME1_PID},${MEME2_PID}"
for MEME_PID in "${MEME_PIDS[@]}"; do
    memtierd-command "swap -pid ${MEME_PID} -status"
    match-pid-pageout "${MEME_PID}" "[2-3][0-9]" 0 1
done

memtierd-meme-stop
memtierd-stop

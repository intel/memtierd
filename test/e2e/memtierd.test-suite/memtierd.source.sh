MEMTIERD_PORT=${MEMTIERD_PORT:-5555}
MEMTIERD_OUTPUT=memtierd.output.txt

memtierd-setup() {
    memtierd-install
    memtierd-reset
    memtierd-os-env
    memtierd-version-check || sleep 5
}

memtierd-install() {
    if ! vm-command "command -v socat"; then
        distro-install-pkg socat
    fi
    if [[ "$reinstall_memtierd" == "1" ]] || ! vm-command "command -v memtierd"; then
        if [ -z "$binsrc" ] || [ "$binsrc" == "local" ]; then
            vm-put-file "${memtierd_src}/bin/memtierd" "$prefix/bin/memtierd"
            vm-put-file "${memtierd_src}/bin/meme" "$prefix/bin/meme"
        else
            error "memtierd-install: unsupported binsrc: '$binsrc'"
        fi
    fi
}

memtierd-reset() {
    vm-command "killall -KILL memtierd meme socat"
}

memtierd-os-env() {
    vm-command "[[ \$(< /proc/sys/kernel/numa_balancing) -ne 0 ]] && { echo disabling autonuma; echo 0 > /proc/sys/kernel/numa_balancing; }"
}

memtierd-version-check() {
    local host_ver
    local vm_ver
    [ -x "${memtierd_src}/bin/memtierd" ] || {
        echo "WARNING"
        echo "WARNING Cannot compare memtierd version on VM to the latest build on host, missing:"
        echo "WARNING ${memtierd_src}/bin/memtierd"
        echo "WARNING"
        echo "WARNING Consider building memtierd for testing: make DEBUG=1 STATIC=1 RACE=1"
        echo "WARNING"
        return 1
    }
    (cd "${memtierd_src}" && (make -q bin/memtierd || make -q STATIC=1 bin/memtierd)) || {
        echo "WARNING"
        echo "WARNING Sources changed, latest build is not up-to-date."
        echo "WARNING"
        echo "WARNING Consider rebuilding memtierd for testing: make DEBUG=1 STATIC=1 RACE=1"
        echo "WARNING"
        return 1
    }
    host_ver=$(${memtierd_src}/bin/memtierd -version)
    vm-command "memtierd -version"
    vm_ver="$COMMAND_OUTPUT"
    if [[ "$host_ver" != "$vm_ver" ]]; then
        echo "WARNING"
        echo "WARNING memtierd version on VM differs from the latest build on host"
        echo "WARNING vm:"
        echo "$vm_ver" | while read l; do echo "WARNING    $l"; done
        echo "WARNING host:"
        echo "$host_ver" | while read l; do echo "WARNING    $l"; done
        echo "WARNING"
        echo "WARNING Consider running tests with reinstall_memtierd=1"
        echo "WARNING"
        sleep 5
        return 1
    fi
}

memtierd-start() {
    if [[ -n "$MEME_CGROUP" ]]; then
        vm-command "echo 0-3 > /sys/fs/cgroup/$MEME_CGROUP/cpuset.mems"
    fi
    if [ -z "${MEMTIERD_YAML}" ]; then
        MEMTIERD_OPTS="-prompt -debug"
    else
        vm-pipe-to-file "memtierd.yaml" <<<"${MEMTIERD_YAML}"
        MEMTIERD_OPTS="-config memtierd.yaml -debug"
    fi
    vm-command "nohup sh -c 'socat tcp4-listen:${MEMTIERD_PORT},fork,reuseaddr - | memtierd ${MEMTIERD_OPTS}' > ${MEMTIERD_OUTPUT} 2>&1 & sleep 2; cat ${MEMTIERD_OUTPUT}"
    vm-command "pgrep memtierd" || {
        command-error "failed to launch memtierd"
    }
}

memtierd-stop() {
    memtierd-command "q"
    sleep 1
    vm-command "killall -KILL memtierd; pkill -f 'socat tcp4-listen:${MEMTIERD_PORT}'"
}

memtierd-command() {
    vm-command "offset=\$(wc -l ${MEMTIERD_OUTPUT} | awk '{print \$1+1}'); echo -e '$1' | socat - tcp4:localhost:${MEMTIERD_PORT}; sleep 1; tail -n+\${offset} ${MEMTIERD_OUTPUT}"
}

memtierd-meme-start() {
    vm-command "nohup meme -bs ${MEME_BS:-1G} -brc ${MEME_BRC:-0} -brs ${MEME_BRS:-0} -bro ${MEME_BRO:-0} -bwc ${MEME_BWC:-0} -bws ${MEME_BWS:-0} -bwo ${MEME_BWO:-0} -ttl ${MEME_TTL:-1h} < /dev/null > meme.output.txt 2>&1 & sleep 2; cat meme.output.txt"
    MEME_PID=$(awk '/pid:/{print $2}' <<<$COMMAND_OUTPUT)
    if [[ -z "$MEME_PID" ]]; then
        command-error "failed to start meme, pid not found"
    fi
    if [[ -n "$MEME_CGROUP" ]]; then
        vm-command "mkdir /sys/fs/cgroup/$MEME_CGROUP; echo \"${MEME_MEMS}\" > /sys/fs/cgroup/$MEME_CGROUP/cpuset.mems; echo $MEME_PID > /sys/fs/cgroup/$MEME_CGROUP/cgroup.procs"
    fi
}

memtierd-meme-stop() {
    vm-command "killall -KILL meme"
    if [[ -n "$MEME_CGROUP" ]]; then
        vm-command "sudo rmdir /sys/fs/cgroup/$MEME_CGROUP"
    fi
}

next-round() {
    local round_counter_var=$1
    local round_counter_val=${!1}
    local round_counter_max=$2
    local round_delay=$3
    if [[ "$round_counter_val" -ge "$round_counter_max" ]]; then
        return 1
    fi
    eval "$round_counter_var=$(($round_counter_val + 1))"
    sleep $round_delay
    return 0
}

memtierd-verify-scanned-pids() {
    local expected_pid_count=$#

    for pid_regexp in "$@"; do
        round_number=0
        while ! (
            memtierd-command "stats -t memory_scans -f csv | awk -F, \"{print \\\$1}\""
            grep ${pid_regexp} <<<$COMMAND_OUTPUT
        ); do
            echo "grep pid matching ${pid_regexp} not found"
            next-round round_number 5 1 || {
                error "timeout: memtierd did not watch ${pid_regexp}"
            }
        done
    done

    memtierd-command 'stats -t memory_scans -f csv'
    observed_pid_count="$(grep ^[0-9] <<<"$COMMAND_OUTPUT" | wc -l)"
    if [[ "$observed_pid_count" != "$expected_pid_count" ]]; then
        error "expected memtierd to watch ${expected_pid_count} pids, but got ${observed_pid_count} pids"
    fi
}

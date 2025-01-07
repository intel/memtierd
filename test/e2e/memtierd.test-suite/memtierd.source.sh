#!/bin/bash

MEMTIERD_PORT=${MEMTIERD_PORT:-5555}
MEMTIERD_OUTPUT=memtierd.output.txt

memtierd-setup() {
    memtierd-install
    memtierd-reset
    memtierd-os-env
    memtierd-version-check || sleep 5
}

# shellcheck disable=SC2154
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
    host_ver=$("${memtierd_src}"/bin/memtierd -version)
    vm-command "memtierd -version"
    vm_ver="$COMMAND_OUTPUT"
    if [[ "$host_ver" != "$vm_ver" ]]; then
        echo "WARNING"
        echo "WARNING memtierd version on VM differs from the latest build on host"
        echo "WARNING vm:"
        echo "$vm_ver" | while read -r l; do echo "WARNING    $l"; done
        echo "WARNING host:"
        echo "$host_ver" | while read -r l; do echo "WARNING    $l"; done
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
        MEMTIERD_OPTS="-prompt -debug -echo"
    else
        vm-pipe-to-file "memtierd.yaml" <<<"${MEMTIERD_YAML}"
        MEMTIERD_OPTS="-config memtierd.yaml -prompt -debug -echo"
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
    local MEMTIERD_PROMPT="\e[38;5;11mmemtierd@vm>\e[0m "
    command-start "memtierd" "$MEMTIERD_PROMPT" "$1"
    vm-command-q "offset=\$(wc -l ${MEMTIERD_OUTPUT} | awk '{print \$1+1}'); echo -e '$1' | socat - tcp4:localhost:${MEMTIERD_PORT}; sleep 1; tail -n+\${offset} ${MEMTIERD_OUTPUT}" | command-handle-output
    command-end "${PIPESTATUS[0]}"
}

memtierd-make-cgroup() {
    # Make sure cgroup exists with default controls
    local cgpath="$1"
    local cgroot="${2:-/sys/fs/cgroup}"
    local cghead="${cgpath%%/*}"
    local cgremainder="${cgpath#*/}"
    if [ -z "$cgpath" ]; then
        return
    fi
    if vm-command-q "[ -d '$cgroot/$cgpath' ]"; then
        return
    fi
    if [ "$cgpath" == "$cgremainder" ]; then
        cgremainder=""
    fi
    vm-command "mkdir -p $cgroot/$cghead; echo +cpuset > $cgroot/$cghead/cgroup.subtree_control"
    memtierd-make-cgroup "$cgremainder" "$cgroot/$cghead"
}

_meme_count=0
memtierd-meme-start() {
    vm-command "nohup meme -bs ${MEME_BS:-1G} -brc ${MEME_BRC:-0} -brs ${MEME_BRS:-0} -bro ${MEME_BRO:-0} -bwc ${MEME_BWC:-0} -bws ${MEME_BWS:-0} -bwo ${MEME_BWO:-0} -ttl ${MEME_TTL:-1h} < /dev/null > meme${_meme_count}.output.txt 2>&1 & sleep 2; cat meme${_meme_count}.output.txt"
    MEME_PID=$(awk '/pid:/{print $2}' <<<"$COMMAND_OUTPUT")
    if [[ -z "$MEME_PID" ]]; then
        command-error "failed to start meme, pid not found"
    fi
    _meme_count=$(( _meme_count + 1 ))
    if [[ -n "$MEME_CGROUP" ]]; then
        memtierd-make-cgroup "$MEME_CGROUP"
        vm-command "mkdir -p /sys/fs/cgroup/$MEME_CGROUP; echo \"${MEME_MEMS}\" > /sys/fs/cgroup/$MEME_CGROUP/cpuset.mems; echo $MEME_PID > /sys/fs/cgroup/$MEME_CGROUP/cgroup.procs"
    fi
}

memtierd-meme-stop() {
    vm-command "killall -KILL meme"
    if [[ -n "$MEME_CGROUP" ]]; then
        vm-command "sleep 1; rmdir /sys/fs/cgroup/$MEME_CGROUP || true"
    fi
}

_python3_count=0
_python3_outputs=()
_python3_ports=()
_python3_pids=()
memtierd-python3-start() {
    PYTHON3_ID=$_python3_count
    _python3_outputs[$PYTHON3_ID]=python3.$PYTHON3_ID.output.txt
    _python3_ports[$PYTHON3_ID]=$(( 33100 + _python3_count ))
    _python3_count=$(( _python3_count + 1 ))
    local PYTHON3_OUTPUT=${_python3_outputs[$PYTHON3_ID]}
    local PYTHON3_PORT=${_python3_ports[$PYTHON3_ID]}
    local put_in_cgroup=""
    if [ -n "$PYTHON3_CGROUP" ]; then
        memtierd-make-cgroup "$PYTHON3_CGROUP"
        put_in_cgroup="echo \$\$ > /sys/fs/cgroup/$PYTHON3_CGROUP/cgroup.procs; "
    fi
    vm-command "nohup sh -c '${put_in_cgroup}socat tcp4-listen:${PYTHON3_PORT},fork,reuseaddr - | (python3 -i -u; pkill -f \"socat tcp4-listen:${PYTHON3_PORT}\")' >& ${PYTHON3_OUTPUT} & sleep 2; cat ${PYTHON3_OUTPUT}; echo;"
    memtierd-python3-command 'import sys; sys.ps1, sys.ps2 = "", ""; print("python3 prompt disabled <<<")'
    memtierd-python3-command "import os; print(os.getpid())"
    _python3_pids[$PYTHON3_ID]=$COMMAND_OUTPUT
}

memtierd-python3-command() {
    local PYTHON3_OUTPUT=${_python3_outputs[$PYTHON3_ID]}
    local PYTHON3_PORT=${_python3_ports[$PYTHON3_ID]}
    local PYTHON3_PROMPT="\e[38;5;11mpython[$PYTHON3_ID]@vm>>>\e[0m "
    if [[ -z "$PYTHON3_PORT" ]]; then
        error "memtierd-python3-command: invalid PYTHON3_ID=$PYTHON3_ID, socat port does not exist"
    fi
    command-start "python3-$PYTHON3_ID" "$PYTHON3_PROMPT" "$1"
    vm-command-q "offset=\$(wc -l ${PYTHON3_OUTPUT} | awk '{print \$1+1}'); echo -e '$1' | socat - tcp4:localhost:${PYTHON3_PORT}; sleep 1; tail -n+\${offset} ${PYTHON3_OUTPUT}" | command-handle-output
    command-end "${PIPESTATUS[0]}"
}

next-round() {
    local round_counter_var=$1
    local round_counter_val=${!1}
    local round_counter_max=$2
    local round_delay=$3
    if [[ "$round_counter_val" -ge "$round_counter_max" ]]; then
        return 1
    fi
    eval "$round_counter_var=$(("$round_counter_val" + 1))"
    sleep "$round_delay"
    return 0
}

# shellcheck disable=SC2034
memtierd-match-scanned-pids() {
    for pid_regexp in "$@"; do
        round_number=0
        while ! (
            memtierd-command "stats -t memory_scans -f csv | awk -F, \"{print \\\$1}\""
            grep "${pid_regexp}" <<<"$COMMAND_OUTPUT"
        ); do
            echo "grep pid matching ${pid_regexp} not found"
            next-round round_number 5 1s || {
                error "timeout: memtierd did not watch ${pid_regexp}"
            }
        done
    done
}

# shellcheck disable=SC2034
memtierd-match-pageout() {
    local pageout_regexp=$1
    local round_counter_max=$2
    local round_delay=$3
    round_number=0
    while ! (
        memtierd-command "stats -t process_madvise -f csv | awk -F, \"{print \\\$6}\""
        grep "${pageout_regexp}" <<<"$COMMAND_OUTPUT"
    ); do
        echo "grep PAGEOUT value matching ${pageout_regexp} not found"
        next-round round_number "${round_counter_max}" "${round_delay}" || {
            error "timeout: memtierd did not pageout expected amount of memory"
        }
    done
}

# shellcheck disable=SC2034
memtierd-match-pagemoving() {
    local pagemoving_regexp=$1
    local round_counter_max=$2
    local round_delay=$3
    round_number=0
    while ! (
        memtierd-command "stats -t move_pages -f csv | awk -F, \"{print \\\$6\\\$7}\""
        grep -E "${pagemoving_regexp}" <<<"$COMMAND_OUTPUT"
    ); do
        echo "grep PAGEMOVING value matching ${pagemoving_regexp} not found"
        next-round round_number "${round_counter_max}" "${round_delay}" || {
            error "timeout: memtierd did not move expected amount of memory"
        }
    done
}

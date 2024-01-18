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

# Test meme process which has 1G of memory and writes actively 300M
# 700M+ should be moved to idlenumas and 300M should be moved to activenumas
MEME_CGROUP=e2e-meme
MEME_BS=1G MEME_BRC=1 MEME_BRS=300M memtierd-meme-start
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
      name: idlepage
      config: |
        pagesinregion: 512
        maxcountperregion: 1
        scanintervalms: 4000
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

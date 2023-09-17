#!/bin/bash
# Make sure the system has IDLEPAGE support enabled
# Note: IDLEPAGE is enabled on ubuntu-22.04 by default
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

match-pagemoving() {
  local pagemoving_regexp=$1
  round_number=0
  while ! (
    memtierd-command "stats -t move_pages -f csv | awk -F, \"{print \\\$6\\\$7}\""
    grep ${pagemoving_regexp} <<<$COMMAND_OUTPUT
  ); do
    echo "grep PAGEMOVING value matching ${pagemoving_regexp} not found"
    next-round round_number 5 9 || {
      error "timeout: memtierd did not expected amount of memory"
    }
  done
}

# Start meme process which has 1G of memory and reads actively 300M
MEME_CGROUP=meme1
MEME_BS=1G MEME_BRC=1 MEME_BRS=300M MEME_MEMS=0 memtierd-meme-start

echo -e "\n=== scenario 1: test moving memory among nodes with policy-ratio and tracker-softdirty ===\n"
MEMTIERD_YAML="
policy:
  name: ratio
  config: |
    intervalms: 4000
    ratio: 0.4
    ratiotargets: [3]
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/${MEME_CGROUP}
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
match-pagemoving "3\:0.3[5-9][0-9]"

memtierd-stop
memtierd-meme-stop

# Start meme process which has 1G of memory and reads actively 300M
MEME_CGROUP=meme1
MEME_BS=1G MEME_BRC=1 MEME_BRS=300M MEME_MEMS=0 memtierd-meme-start

echo -e "\n=== scenario 2: test moving memory among nodes with policy-ratio and tracker-idlepage ===\n"
MEMTIERD_YAML="
policy:
  name: ratio
  config: |
    intervalms: 4000
    ratio: 0.4
    ratiotargets: [3]
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/${MEME_CGROUP}
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
match-pagemoving "3\:0.3[5-9][0-9]"

memtierd-stop
memtierd-meme-stop

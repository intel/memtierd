#!/bin/bash

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

# start 3 meme processes
for i in {0..2}; do
    MEME_BS=1G MEME_BWC=1 MEME_BWS=300M MEME_BRC=1 MEME_BRS=300M memtierd-meme-start
    PID_VAR="MEME${i}_PID"
    eval "${PID_VAR}=${MEME_PID}"
done

echo -e "\n=== scenario 1: test filter/proc with policy-age and multi-tracker (softdirty and idlepage) ===\n"
MEMTIERD_YAML="
policy:
  name: age
  config: |
    intervalms: 4000
    pidwatcher:
      name: filter
      config: |
        source:
          name: proc
          config: |
            intervalms: 4000
        filters:
          - procexeregexp: '.*/meme'
    idledurationms: 8000
    idlenumas: [3]
    activedurationms: 6000
    activenumas: [1]
    tracker:
      name: multi
      config: |
        trackers:
          - name: idlepage
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 4000
          - name: softdirty
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 4000
    mover:
      intervalms: 20
      bandwidth: 200
"
memtierd-start

sleep 5
memtierd-match-scanned-pids "$MEME0_PID" "$MEME1_PID" "$MEME2_PID"

memtierd-stop

echo -e "\n=== scenario 2: test filter/proc with policy-heat and multi-tracker (softdirty and idlepage) ===\n"
# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: heat
  config: |
    intervalms: 4000
    pidwatcher:
      name: filter
      config: |
        source:
          name: proc
          config: |
            intervalms: 4000
        filters:
          - procexeregexp: '.*/meme'
    heatnumas:
      0: [-1]
    heatmap:
      heatmax: 0.01
      heatretention: 0
      heatclasses: 5
    tracker:
      name: multi
      config: |
        trackers:
          - name: idlepage
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 4000
          - name: softdirty
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 4000
    mover:
      intervalms: 20
      bandwidth: 1000
"
memtierd-start

sleep 5
memtierd-match-scanned-pids "$MEME0_PID" "$MEME1_PID" "$MEME2_PID"

memtierd-stop

echo -e "\n=== scenario 3: test filter/proc (negative case) with policy-age and multi-tracker (softdirty and idlepage) ===\n"
# shellcheck disable=SC2034
MEMTIERD_YAML="
policy:
  name: age
  config: |
    intervalms: 4000
    pidwatcher:
      name: filter
      config: |
        source:
          name: proc
          config: |
            intervalms: 4000
        filters:
          - procexeregexp: '.*/non-existent'
    idledurationms: 8000
    idlenumas: [3]
    activedurationms: 6000
    activenumas: [1]
    tracker:
      name: multi
      config: |
        trackers:
          - name: idlepage
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 4000
          - name: softdirty
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 4000
    mover:
      intervalms: 20
      bandwidth: 200
"
memtierd-start

sleep 5
memtierd-match-scanned-pids

memtierd-stop

memtierd-meme-stop

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

# start meme
for i in {0..2}; do
    MEME_CGROUP="meme${i}" MEME_BS=1G MEME_BWC=1 MEME_BWS=300M MEME_MEMS=0 memtierd-meme-start
    PID_VAR="MEME${i}_PID"
    eval "${PID_VAR}=\$MEME_PID"
    CGROUP_VAR="MEME${i}_CGROUP"
    eval "${CGROUP_VAR}=\meme${i}"
done

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

match-pids() {
    for pid_regexp in "$@"; do
      round_number=0
      while ! ( memtierd-command "stats -t memory_scans -f csv | awk -F, \"{print \\\$1}\""; grep ${pid_regexp} <<< $COMMAND_OUTPUT); do
          echo "grep pid matching ${pid_regexp} not found"
          next-round round_number 5 1 || {
              error "timeout: memtierd did not watch ${pid_regexp}"
          }
      done
    done
}

echo -e "\n=== scenario 1: policy-age and multi-tracker (softdirty and idlepage) ===\n"
MEMTIERD_YAML="
policy:
  name: age
  config: |
    intervalms: 4000
    pidwatcher:
      name: filter
      config: |
        source:
          name: cgroups
          config: |
            intervalms: 4000
        filters:
          - procexeregexp: ".*/meme"
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
match-pids $MEME0_PID $MEME1_PID $MEME2_PID

memtierd-stop

echo -e "\n=== scenario 2: policy-heat and multi-tracker (softdirty and idlepage) ===\n"
MEMTIERD_YAML="
policy:
  name: heat
  config: |
    intervalms: 4000
      name: filter
      config: |
        source:
          name: cgroups
          config: |
            intervalms: 4000
        filters:
          - procexeregexp: ".*/meme"
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
match-pids $MEME0_PID $MEME1_PID $MEME2_PID

memtierd-stop

memtierd-meme-stop

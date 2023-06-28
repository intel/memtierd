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
    MEME_CGROUP="meme${i}" MEME_BS=1G MEME_BWC=1 MEME_BWS=300M MEME_MEMS="" memtierd-meme-start
    PID_VAR="MEME${i}_PID"
    eval "${PID_VAR}=${MEME_PID}"
    CGROUP_VAR="MEME${i}_CGROUP"
    eval "${CGROUP_VAR}=meme${i}"
done

echo -e "\n=== scenario 1: test cgroups with policy-age and multi-tracker (softdirty and idlepage) ===\n"
MEMTIERD_YAML="
policy:
  name: age
  config: |
    intervalms: 4000
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/$MEME0_CGROUP
          - /sys/fs/cgroup/$MEME1_CGROUP
          - /sys/fs/cgroup/$MEME2_CGROUP
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
memtierd-verify-scanned-pids $MEME0_PID $MEME1_PID $MEME2_PID

memtierd-stop

echo -e "\n=== scenario 2: test cgroups with policy-heat and multi-tracker (softdirty and idlepage) ===\n"
MEMTIERD_YAML="
policy:
  name: heat
  config: |
    intervalms: 4000
    pidwatcher:
      name: cgroups
      config: |
        cgroups:
          - /sys/fs/cgroup/$MEME0_CGROUP
          - /sys/fs/cgroup/$MEME1_CGROUP
          - /sys/fs/cgroup/$MEME2_CGROUP
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
memtierd-verify-scanned-pids $MEME0_PID $MEME1_PID $MEME2_PID

memtierd-stop

memtierd-meme-stop

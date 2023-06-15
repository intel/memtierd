memtierd-setup

# Test meme process which has 1G of memory and writes actively 300M
# 700M+ should be moved to idlenumas and 300M should be moved to activenumas
MEME_CGROUP=e2e-meme
MEME_BS=1G MEME_BWC=1 MEME_BWS=300M MEME_MEMS=0 memtierd-meme-start
sleep 2

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
      bandwidth: 200
"
memtierd-start

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

match-pagemoving() {
    local pagemoving_regexp=$1
    round_number=0
    while ! ( memtierd-command "stats -t move_pages -f csv | awk -F, \"{print \\\$6\\\$7}\""; grep ${pagemoving_regexp} <<< $COMMAND_OUTPUT); do
        echo "grep PAGEMOVING value matching ${pagemoving_regexp} not found"
        next-round round_number 5 9 || {
            error "timeout: memtierd did not expected amount of memory"
        }
    done
}

sleep 4
round_number=0
echo "waiting 200M+ (active) to be moved to node 1 and 700M+ (idle) to be moved to node 3"
match-pagemoving "1\:0.2[0-9][0-9]"
match-pagemoving "3\:0\.7[0-9][0-9]"

echo "stop meme, expect all memory to be moved to node 3"
vm-command "kill -STOP ${MEME_PID}"
match-pagemoving "3\:1\.[0-2]"

echo "continue meme, expect ~300 MB to be moved to node 1"
vm-command "kill -CONT ${MEME_PID}"
match-pagemoving "1\:0.5[0-9][0-9]"

echo "stop meme again, expect ~300 MB to be moved to node 3"
vm-command "kill -STOP ${MEME_PID}"
match-pagemoving "3\:1\.[2-4]"

memtierd-stop
memtierd-meme-stop

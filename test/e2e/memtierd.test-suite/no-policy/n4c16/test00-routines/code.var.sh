#!/bin/bash

TMPDIR=/tmp/memtierd-e2e-$(basename "$TEST_DIR")
TMPSHELL=$TMPDIR/shell.output.txt
TMPMEMCMD=$TMPDIR/memtierd-command.output.txt
vm-command "mkdir -p '$TMPDIR'; rm -f '$TMPSHELL' '$TMPMEMCMD'"

memtierd-setup

# # Test that 700M+ is swapped out from a process that has 1G of memory
# # and writes actively 300M.
# MEME_CGROUP=e2e-meme MEME_BS=1G MEME_BWC=1 MEME_BWS=300M memtierd-meme-start

MEMTIERD_YAML="
routines:
  - name: statactions
    config: |
      intervalms: 1000
      intervalcommand: ['sh', '-c', 'date +%F-%T >> $TMPSHELL']
policy:
  name: stub
"
memtierd-start
sleep 4
memtierd-stop
vm-command "wc -l $TMPSHELL | awk '{print \$1}'"
[[ "$COMMAND_OUTPUT" -gt 2 ]] || {
    command-error "expected more than 2 lines in $TMPSHELL"
}

MEMTIERD_YAML="
routines:
  - name: statactions
    config: |
      intervalms: 200
      intervalcommandrunner: memtier
      intervalcommand: ['stats', '-t', 'events']
policy:
  name: stub
"
memtierd-start
sleep 4
memtierd-stop
vm-command "grep RoutineStatActions.command.memtier $MEMTIERD_OUTPUT | tail -n 1"
[[ $(awk '{print $1}' <<<"$COMMAND_OUTPUT") -gt 15 ]] || {
    command-error "at least 15 memtier command executions expected"
}

# shellcheck disable=SC2034
MEMTIERD_YAML="
routines:
  - name: statactions
    config: |
      intervalms: 1000
      intervalcommandrunner: memtier-prompt
      intervalcommand: [\"stats -t events\"]
      timestamp: \"before: epochs: unix, unix.s, unix.milli, unix.micro and unix.nano or duration, duration.s, duration.milli, duration.micro, duration.nano\n
          or a template: 2006-01-02T15:04.05 -0700 or a string like\n unixdate rfc822z rfc3339nano or rfc3339\n\"
      timestampafter: \"after: rfc3339\n\"
policy:
  name: stub
"
memtierd-start
sleep 2.5
memtierd-stop
vm-command "grep -E 'before: epochs: [12].*' $MEMTIERD_OUTPUT | tail -n 1" || {
    command-error "could not find timestamp before command"
}
vm-command "grep -E 'after:' $MEMTIERD_OUTPUT | tail -n 1" || {
    command-error "could not find timestamp after command"
}
vm-command "grep -B2 -E 'after:' $MEMTIERD_OUTPUT | tail -n 3 | grep RoutineStatActions" || {
    command-error "could not find command output before timestamp after command"
}
vm-command "grep -A3 -E 'before: epochs: [12].*' $MEMTIERD_OUTPUT | tail -n 1 | grep 'table: events'" || {
    command-error "table: events is missing three lines after before epochs"
}
vm-command "! ( grep nano $MEMTIERD_OUTPUT || grep rfc $MEMTIERD_OUTPUT || grep unix $MEMTIERD_OUTPUT || grep duration $MEMTIERD_OUTPUT ) " || {
    command-error "timestamp words remained unreplaced"
}

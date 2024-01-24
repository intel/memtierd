#!/bin/bash

thp-enable() {
    vm-command '
    THPEN=/sys/kernel/mm/transparent_hugepage/enabled
    set -x
    grep -q "\[always\]" $THPEN || echo always > $THPEN
    cat $THPEN'
    if ! grep -q "\[always\]" <<< "$COMMAND_OUTPUT"; then
        error "enabling transparent hugepages failed"
    fi
}

thp-disable() {
    vm-command '
    THPEN=/sys/kernel/mm/transparent_hugepage/enabled
    set -x
    grep -q "\[never\]" $THPEN || echo never > $THPEN
    cat $THPEN'
    if ! grep -q "\[never\]" <<< "$COMMAND_OUTPUT"; then
        error "disabling transparent hugepages failed"
    fi
}

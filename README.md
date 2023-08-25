# Memtierd

Memtierd is a daemon and command line utility for tracking, moving and
swapping memory on Linux.

When running as a daemon, memtierd finds chosen processes from the
whole system or selected cgroups, tracks accesses to their memory
regions, moves data to different NUMA nodes or swaps it out, based on
last access times or access frequency.

When running in interactive prompt, available commands control
trackers and policies, show statistics on performance, last access
times and memory heat classification. Furthermore, commands enable
immediate memory moves, and swapping data out and back into RAM.

This project includes
- [memtierd daemon](cmd/memtierd/README.md)
- [memtier Go library](pkg/memtier/README.md)

## Install

Build and install the latest memtierd:

```
go install github.com/intel/memtierd/cmd/memtierd@latest
```

This installs the `memtierd` binary to the directory pointed by
`$GOBIN`, that defaults to `$GOPATH/bin` or `$HOME/go/bin`.


## Releases

Every release of this project is at the same time a memtier Go library
and a memtierd daemon release. Releases are tagged as `v0.MAJOR.MINOR`
in the main branch.

New major releases include new features and/or other major changes
that may break backwards compatibility in API, configuration or
command line interface. Breaks, if any, are reported in release notes.

New minor versions do not break backwards compatibility and they
contain only bug fixes and/or minor improvements.

All releases are made on a need basis.

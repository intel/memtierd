# memtierd - daemon for managing memory in userspace

Memtierd is a userspace daemon that manages memory of chosen
processes. Memtierd can swap memory in and out, and move memory
between NUMA nodes. Moving enables both promotion and demotion of
pages, that is, moving actively used pages to low-latency memory, and
idle pages away from low-latency memory to free it for better use.

Memtierd includes multiple memory trackers and policies. A tracker
counts accesses in memory regions, while a policy classifies the pages
based on observed accesses: is a page active, idle, or somewhere
between.

The granularity of memtierd trackers and memory classifications is
configurable, and often significantly larger than a single page. For
simplicity, this document talks about "pages", but most often this
means an address range that contains one or more pages.

## Architecture

Following architecture diagram shows how memtierd components
communicate together, user and system.

[![Architecture](../../docs/architecture.svg)]


## Build

```
make bin/memtierd
```

## Usage

Memtierd starts in an automatic mode with a configuration file, or in
a command mode with interactive prompt.

### Automatic mode

In the automatic mode memtierd configures a policy that starts
managing pages. Memtierd enters to the automatic mode when launched
with a configuration file that includes policy and tracker parameters:

```
memtierd -config FILE
```

See configuration samples below.

- [memtierd-age-idlepage-trackonly.yaml](../../sample-configs/memtierd-age-idlepage-trackonly.yaml)
  tracks processes in `/sys/fs/cgroup/track-me` but does not swap out or
  move memory. Useful for understanding memory access time
  demographics. Example:
  ```
  # (while :; do sleep 5; echo policy -dump accessed 0,5s,30s,10m,2h,24h,0; done) | memtierd -config memtierd-age-idlepage-trackonly.yaml -prompt
  ...
  memtierd> policy -dump accessed 0,5s,30s,10m,2h,24h,0
  table: time since last access
       pid lastaccs>=[s] lastaccs<[s]    pages   mem[G] pidmem[%]
   2888906         0.000        5.000   318574    1.215     14.64
   2888906         5.000       30.000   755200    2.881     34.72
   2888906        30.000      600.000  1101542    4.202     50.64
   2888906       600.000     7200.000        0    0.000      0.00
   2888906      7200.000    86400.000        0    0.000      0.00
   2888906     86400.000        0.000        0    0.000      0.00
  ```

- [memtierd-heat-damon.yaml](../../sample-configs/memtierd-heat-damon.yaml)
  configures the heat policy to use the damon tracker.

- [memtierd-heat-idlepage.yaml](../../sample-configs/memtierd-heat-idlepage.yaml)
  configures the heat policy to use the idlepage tracker.

- [memtierd-age-softdirty.yaml](../../sample-configs/memtierd-age-softdirty.yaml)
  configures the age policy to use the softdirty tracker.

Policies are described in the [Policies](#policies) section.

### Command mode

In the command mode memtierd reads user commands from the standard
input and prints results to the standard output. Memtierd enters to
the command mode when launched with `-prompt`. The command mode prints
help on available commands with `help`, and on parameters of a command
with `COMMAND -h`:

```
memtierd -prompt
memtierd> help
memtierd> pages -h
```

If a command includes a pipe (`|`), the right-hand-side of the first
pipe will be run in a shell, and the output of the left-hand-side of
the pipe will be piped to the shell command:

```
memtierd> stats | grep accessed
```

Command mode prompt is available in automatic mode, too, when both
`-config FILE` and `-prompt` options are given. This gives interactive
access to statistics and running policy, tracker and mover.

Example: Start moving all pages of the `meme` process to NUMA
node 1. After 10 seconds print statistics and quit:

```
( echo "pages -pid $(pidof meme)"; echo "mover -pages-to 1"; sleep 10; echo "stats"; echo "q" ) | memtierd -prompt
```

Example: Use idlepage tracker to track the memory of the `meme`
process. After 10 seconds print detected accesses and statistics:

```
( echo "tracker -create idlepage -start $(pidof meme)"; sleep 10; echo "tracker -counters"; echo "stats" ) | ./memtierd -prompt
```

Example: Save timestamped raw memory access events that a tracker has
recorded.

```
# Start recording raw memory access events.
memtierd> tracker -dump raw start
# Append new (unreported) raw memory access events to a file.
memtierd> tracker -dump raw new | tee -a raw-events.txt | wc -l
...
memtierd> tracker -dump raw new | tee -a raw-events.txt | wc -l
# Finally stop recording.
memtierd> tracker -dump raw stop
```

> Tip: install `rlwrap` and run `sudo rlwrap memtierd --prompt` to
> enable convenient readline input with history.

## Test environment

E2E test framework allows specifying hardware topology, including the
number of CPUs and amount of memory in each NUMA nodes, and choosing
Linux distribution to be installed on the VM.

The e2e test framework uses
[govm](https://github.com/govm-project/govm) that runs Qemu VMs in
docker containers.

### Install e2e test framework dependencies on host

1. docker
2. govm

   Example on building govm and govm/govm:latest Docker image on Ubuntu:
   ```
   sudo apt install -y docker.io git-core golang
   export GOPATH=$HOME/go
   export PATH=$PATH:$GOPATH/bin
   GO111MODULE=off go get -d github.com/govm-project/govm && cd $GOPATH/src/github.com/govm-project/govm && go mod tidy && go mod download && go install && cd .. && docker build govm -f govm/Dockerfile -t govm/govm:latest
   ```

### Create a VM with the topology of your interest

Example of a four-NUMA-node VM with:
- 2 NUMA nodes with 4 CPUs (2 cores with 2 hyperthreads each) / 4G memory on each node
- 2 NUMA nodes with 0 CPUs / 2G memory on each node

```
topology='[{"cores": 2, "threads": 2, "mem": "4G", "nodes": 2}, {"cores":0, "mem":"2G", "nodes": 2}]' distro=debian-sid vm=debian-4422 on_vm_online='interactive; exit' test/e2e/run.sh interactive
```

See supported Linux distributions and other options with
`test/e2e/run.sh help`.

You can get help on all available commands in the interactive prompt
and in the scripts:

```
test/e2e/run.sh help script all
```

> Tip: installing a custom kernel to the VM
>
> If you wish to install packages from the host filesystem to the VM,
> you can use `vm-put-pkg`. This works both manually in the interactive
> prompt, and in scripts in the `on_vm_online` environment variable.
>
> Example: Install a kernel package, reboot the VM and start the
> interactive prompt once the VM has rebooted. Finally exit the test
> framework when the user quits the interactive prompt.
> ```
> on_vm_online='vm-put-pkg kernel-default-5.15*rpm && vm-reboot && interactive; exit'
> ```

### Install memtierd on the VM

Use `govm ls` on the host to find out the IP address of the VM where
to install `memtierd`

```
scp bin/memtierd debian@172.17.0.2:
ssh debian@172.17.0.2 "sudo mv memtierd /usr/local/bin"
```

Optional: `meme` is a memory exerciser program, developed for
`memtierd` testing and development. You can build and install it as
follows:

```
make bin/meme
scp bin/meme debian@172.17.0.2:
ssh debian@172.17.0.2 "sudo mv meme /usr/local/bin"
```

### Use memtierd in the VM

1. Login to the VM (use `govm ls` to find the IP address of the correct VM):
   ```
   ssh opensuse@172.17.0.2
   ```

   Note: all commands below in this section are executed on the VM.

   You can use `numactl` to inspect the topology and free memory on each NUMA node
   ```
   sudo apt install numactl
   sudo numactl -H
   ```

2. Create a process that uses a lot of memory.

   Example: with the following command `meme` allocates 1 GB of
   memory. The first 128M is only read, next 128M is read and written,
   the next 128M is only written, and the remaining 640M is idle:

   ```
   meme -bs 1G -brs 256M -bws 256M -bwo 128M
   ```

   See `meme -h` for more options.

3. Start memtierd

   Command mode

   ```
   sudo memtierd -prompt
   ```

   Automatic mode

   ```
   sudo memtierd -config FILE
   ```

4. Observe how process's memory is managed.

   - `/proc/PID/numa_maps` includes the number of process's memory pages on each NUMA node.

   - `/sys/fs/cgroup/.../memory.numa_stat` includes the number of
     bytes of memory of all processes in a cgroup (in cgroup v2).

     > Tip: `awk` spells for parsing and summarizing the files above:
     > ```
     > # Total memory of MYPROCESS (note: assuming page size of 4 kB)
     > awk 'BEGIN{RS=" ";FS="="}/N[0-9]+/{mem[$1]+=$2}END{for(node in mem){print node" "mem[node]*4/1024" M"}}' < /proc/$(pidof MYPROCESS)/numa_maps
     >
     > # Anonymous memory of all processes in a cgroup
     > awk 'BEGIN{RS=" ";FS="="}/N[0-9]+/{mem[$1]+=$2}END{for(node in mem){print node" "mem[node]/1024/1024" M"}}' <(grep ^anon /sys/fs/cgroup/.../memory.numa_stat)
     > ```

  - The `stats` command in the `memtierd` prompt reports memory
    scanning times and summarizes amount of memory moved to each
    node.

    ```
    memtierd> stats
    ```

## Policies

Memtierd implements four policies: age, heat, ratio, and
avoid-oom. Age and heat policies move or swap out memory based on last
access times (age) or memory activity class (heat class). The ratio
policy moves or swaps out a fixed ratio of least recently used
memory. The avoid-oom policy tracks cpuset.mems from cgroups, and
moves memory between nodes in case of raising risk of kernel
out-of-memory killer.

## Watchers

Policies track and manage the memory of processes of
interest. Processes are found and filtered by pidwatchers. The cgroups
pidwatcher looks for processes under listed cgroups directories. The
proc pidwatcher finds all running processes in the system. The pidlist
pidwatcher finds only the static list of pids. The filter pidwatcher
filters interesting processes out of all processes found by other
pidwatchers.

For examples of configuring pidwatchers as part of policy
configuration, see:

[memtierd-age-idlepage-trackonly.yaml](../../sample-configs/memtierd-age-idlepage-trackonly.yaml).

### Pidwatcher: cgroups

Finds and watches pids in cgroups.

Configuration options:
- `intervalms` (int): poll interval (ms) for changes in pids in listed
  cgroup paths.
- `cgroups` (list of strings): list of absolute cgroup paths to be
  polled. The pidwatcher will watch all pids found in `cgroup.procs`
  files in these directories and their subdirectories, recursively.

### Pidwatcher: proc

Finds and watches pids in the system.

Configuration options:
- `intervalms` (int): poll interval (ms) for changes in pids under
  `/proc`

### Pidwatcher: pidlist

Finds a static set of pids.

Configuration options:
- `pids` (list of ints): list of pids.

### Pidwatcher: filter

Filters pids reported by the source pidwatcher.

Configuration options:
- `source` (object) specifies and configures the source pidwatcher:
  - `name` (string): name of the source pidwatcher ("proc", "cgroups", ...)
  - `config` (string): configuration of the source pidwatcher.
- `filters` (list of filter objects). The pidwatcher finds pids that
  match by any of listed filter objects. Each filter object contains
  one or more filter criteria. A pid must comply with all criteria in
  a single filter object or be filtered out. In other words, `filters`
  is logical "or" on top level, logical "and" in multi-criteria filter
  objects. Explicit "and", "or", and "not" criteria are available for
  more complex scenarios. Available filter criteria:
  - `procexeregexp` (string): regular expression that matches process
    executable filepath (/proc/pid/exe)
  - `minvmsizekb` (int): minimum /proc/pid/status VmSize (total memory
    size), smaller are filtered out. VmSize includes private and
    shared data in RAM and disks.
  - `minvmrsskb` (int): minimum /proc/pid/status VmRSS (resident size
    in memory), smaller are filtered out. VmRSS includes both private
    and shared memory in RAM.
  - `minprivatedirtykb` (int): minimum private dirty size in
    /proc/pid/smaps.
  - `not` (filter object): inverse matching. Pids that match given
    filter object are filtered out.
  - `and` (list of filter objects): pids that do not match all of the
    listed filter objects will be filtered out.
  - `or` (list of filter objects): pids that do not match any of the
    listed filter objects will be filtered out.

### The age policy

The age policy keeps record on two times:

1. Idle time: how long a time a page has been completely idle.

2. Active time: how long a time a page has been active every time when
   checked.

If the idle time exceeds `IdleDurationMs` in the policy configuration,
the page is moved a node in `IdleNumas` (demotion). If the active time
exceeds `ActiveDurationMs`, the page is moved to a node in
`ActiveNumas` (promotion). Demotion and promotion are disabled if the
corresponding duration equals 0, or if the list of corresponding nodes
is empty.

Example: a page is idle if a tracker has not seen activity in the past
15 seconds. On the other hand, a page is active if tracker has seen
activity on every scan in the past 10 seconds. In both cases pages are
moved.

```
memtierd> policy -create age -config {"Tracker":{"Name":"softdirty","Config":"{\"PagesInRegion\":256,\"MaxCountPerRegion\":1,\"ScanIntervalMs\":4000,\"RegionsUpdateMs\":0,\"SkipPageProb\":0,\"PagemapReadahead\":0}"},"Mover":{"IntervalMs":20,"Bandwidth":200},"Cgroups":["/sys/fs/cgroup/foobar"],"IntervalMs":5000,"IdleDurationMs":15000,"IdleNumas":[2,3],"ActiveDurationMs":10000,"ActiveNumas":[0,1]} -start
```

The age policy works with idlepage and softdirty trackers, but not
with the damon tracker.

### The heat policy

The heat policy stores tracker counters into a heatmap, and moves
pages based on their heat.

The heatmap quantifies heats of pages (heats values from 0.0 to
`HeatMax`) into classes. The number of classes is specified with
`HeatClasses`. The heat classes are named 0, 1, ..., `HeatClasses`-1,
the last being the hottest. `HeatRetention` is the portion of the heat
that retains in the map after one second of inactivity.

The policy parameter `HeatNumas` maps heat classes into sets of NUMA
nodes. A page that belongs to a class should be moved into any NUMA
node associated with this class. If a heat class is missing from the
`HeatNumas` map, a page in that heat class will not be moved.

Example: divide pages into four heat classes: 0, 1, 2 and 3. Move
hottest pages (class 3) to nodes 0 or 1, and coldest pages (class 0)
to 2 or 3, and leave intermediate pages unmoved.

```
memtierd> policy -create heat -config {"Tracker":{"Name":"idlepage","Config":"{\"PagesInRegion\":256,\"MaxCountPerRegion\":0,\"ScanIntervalMs\":5000,\"RegionsUpdateMs\":0,\"PagemapReadahead\":0,\"KpageflagsReadahead\":0,\"BitmapReadahead\":0}"},"Heatmap":{"HeatMax":0.01,"HeatRetention":0.8,"HeatClasses":4},"Mover":{"IntervalMs":20,"Bandwidth":200},"Cgroups":["/sys/fs/cgroup/foobar"],"IntervalMs":10000,"HeatNumas":{"0":[2,3],"3":[0,1]}}
```

The heat policy works with all trackers.

### The avoid-oom policy

The purpose of the avoid-oom policy is to balance memory between sets
of NUMA nodes so that Linux oom-killer will not kill processes due to
out-of-memory.

Example: a critical low-latency process running on a two-socket system
is allowed to use memory only from NUMA nodes 0-1 as they have low
enough latency to the CPU package 0 where the process is running. On
the other hand, best effort processes are allowed to use all other
CPUs from both packages, and memory from any NUMA node (0-3) in the
same system. If best effort processes have consumed almost all memory
from nodes 0-1 and the low-latency process requests more memory than
available on these nodes, oom-killer resolves the out-of-memory
situation by killing one or more processes that use memory on these
nodes. If the avoid-oom policy detects this risk early enough, it
transparently moves memory of best effort processes to from NUMA nodes
0-1 to 2-3, avoiding out-of-memory even before throttling memory
allocations of best effort processes.

The avoid-oom policy keeps tracking amount of free memory in all sets
of NUMA nodes that it finds in tracked cgroups. The policy divides
nodes into high, medium and no pressure nodes. If there is a set of
NUMA nodes where total available memory has fallen below policy's
"start moving" watermark, all nodes in the set are under high
pressure. On the other hand, if the total available memory of a node
set is above start but below stop moving watermarks, nodes in the set
are under medium pressure at minimum. Nodes that are not under high or
medium pressure are "no pressure" nodes.

The policy moves memory from high to no pressure node sets. If there
is enough available memory in no pressure nodes, this is done until
high pressure has changed into no pressure. The policy searches for
processes that use memory from any of the high pressure nodes but are
allowed to use memory from no pressure nodes, too. When such a process
is found, memory pages in high pressure nodes is moved to no pressure
nodes. If there are many, moving processes with highest OOM score is
prioritized.

Configuration options:

- `StartFreeingMemory` and `StopFreeingMemory`: if available memory on
  a node set falls below `StartFreeingMemory`, the policy tries to
  move memory until it raises above `StopFreeingMemory`. Values are
  either absolute amounts of available memory, for instance: "16G", or
  percentage of total memory in a node set, for instance "25%".

- `Cgroups`: list of absolute cgroup v2 paths. All listed paths are
  searched recursively to find NUMA node sets (cpuset.mems) and
  processes whose memory can be moved (cgroups.procs).

- `IntervalMs` interval how often the policy polls cgroups, memory
  available on NUMA nodes, and triggers moves if needed and possible.

- `Mover`:
  - `Bandwidth`: integer, memory balancing bandwidth limit, MB/s.
  - `IntervalMs`: integer, interval of move_pages syscalls when moving
    memory, milliseconds. The shorter the interval the smaller number
    of pages is moved on each call and the shorter the move time, but
    the higher the overhead due to more frequent syscalls and memory
    locking.

Configuration example:

```
policy:
  name: avoid-oom
  config: |
    intervalms: 5000
    startfreeingmemory: 10%
    stopfreeingmemory: 15%
    cgroups:
    - /sys/fs/cgroup/kubepods.slice
    mover:
      intervalms: 100
      bandwidth: 250
```

## Trackers

Trackers track memory activity of a set of processes. List of
trackers, what they detect and their dependencies.

- damon:
  - Detects reads and writes.
  - Kernel configuration: `DAMON`, `DAMON_SYSFS` (or `DAMON_DBGFS`)
  - Userspace interface:
    - `/sys/kernel/mm/damon/admin` for configuring DAMON.
    - The `bpftrace` tool for reading access data.
- idlepage:
  - Detects reads and writes.
  - Kernel configuration: `IDLE_PAGE_TRACKING`
  - Userspace interface:
    - `/sys/kernel/mm/page_idle/bitmap`
- softdirty:
  - Detect only writes.
  - Kernel configuration: `MEM_SOFT_DIRTY`
  - Userspace interface:
    - `/proc/PID/clear_refs`
    - `/proc/PID/pagemap`
- multi:
  - Combination of trackers.

### The damon tracker

The damon tracker uses DAMON (data access monitor) in Linux kernel for
tracking memory activity of processes. The tracker takes following
parameters in its `config`:

- `connection` specifies how to the tracker reads access data from the
  kernel. The default is "bpftrace", that is recommended and works
  with 6.X Linux kernels. "perf" is an alternative for the first DAMON
  versions in Linux kernels 5.15 and 5.16. Both connect to the
  `damon_aggregation` tracepoint.

- `kdamondslist` specifies which kdamond instances in the system (see
  `/sys/kernel/mm/damon/admin/kdamonds`) are used by this damon
  tracker instance. This option has no effect if using legacy debugfs
  interface. Example: track memory using two kernel threads:
  kdamond.3 and kdamond.5: `kdamondslist: [3, 5]`.

- `nrkdamonds` specifies how many kdamond instances are initialized in
  the system in case there is currently 0 in
  `/sys/kernel/mm/damon/admin/kdamonds/nr_kdamonds`.  The number
  should be sufficient for all damon trackers that may run in the
  system, and should not be updated once it is initialized, because
  changing the value is not possible when there are `kdamond` threads
  running. If this file contains a non-zero value, the damon tracker
  considers this system parameter to be managed by someone else, and
  will not change it. Example: `nrkdamonds: 8` allows using values 0-7
  in `kdmaondslist`'s of damon tracker configurations in the
  system. The default is 0, that is, someone else like system admin
  is expected to initialize `nr_kdamonds` to be large enough to
  include kdamonds specified in `kdamondslist`.

- `interface` specifies the configuration interface of DAMON. Value 0
  is autodetect: prefer `sysfs` and fallback to `debugfs` if not
  available. Value 1 forces using `sysfs`, value 2 `debugfs`. The
  default is 0.

- `filteraddressrangesizemax` specifies the maximum length for address
  ranges which DAMON reports having similar access pattern. Limiting
  the size ignores most cases where DAMON reports accesses in
  non-contiguous virtual address ranges, and cases where the address
  range is condered to be too large to be accurate. Value -1 is
  unlimited. The default is 33554432 (that is 32 MB).

While parameters above configure the DAMON tracker in memtierd,
parameters below are direct pass-through parameters to DAMON
configuration interfaces, both sysfs and debugfs. Refer to monitoring
attributes the [DAMON
documentation](https://docs.kernel.org/admin-guide/mm/damon/usage.html)
for more information.

- `samplingus` is the sampling interval in microseconds.
   The default is 5000.
- `aggregationus` is the aggregation interval in microseconds.
   The default is 100000.
- `regionsupdateus` is the regions update interval in microseconds.
   The default is 1000000.
- `mintargetregions` is the minimum monitoring target regions.
   The default is 10.
- `maxtargetregions` is the maximum monitoring target regions.
   The default is 1000.

### The idlepage tracker

The idlepage tracker handles memory in regions of a configurable
size. It scans all regions once in every `scanintervalms`, and reports
the number of non-idle pages of each region. The idlepage tracker can
be configured with the following parameters:

- `pagesinregion` specifies the size of every memory region. The
  default is 512 (that is 2 MB).
- `scanintervalms` idlepage bit scanning interval in milliseconds. The
  default is 5000.
- `maxcountperregion` is the maximum number of non-idle pages reported
  on each region. Values greater than `pagesinregion` are not
  sensible. Value 0 is unlimited. The default is 1, that is, skip the
  rest of the pages in region immediately when one non-idle page is
  found, and report at most 1 non-idle page for every region. Note
  that when Linux kernel uses THP (transparent huge pages), this
  default gives uniform scoring for normal and THP memory regions.
- `regionsupdatems` specifies how often new memory regions of tracked
  processes are searched for. Value 0 means updating them every time
  before new scan. The default is 10000.

Use `stats -t memory_scans` in the memtierd prompt to see how long it
takes to scan the memory of tracked processes. This helps adjusting
intervals suitable for the workload at hand.

### The softdirty tracker

The softdirty tracker handles memory like the idlepage tracker and
takes exactly the same parameters (`pagesinregion`, `scanintervalms`,
`maxcountperregion`, `regionsupdatems`). Instead of the idlepage bit,
the softdirty tracker uses the softdirty bit of every page, that is
way faster to read, but it is changed only by writing the memory. This
tracker is good choice if memory management decisions can be made
based on write accesses, for instance, migrating mostly read-only
pages to slow-to-write memory.

### The multi tracker

The multi tracker combines several trackers. It is configured with a
list of trackers that it will run and whose findings it will combine.

Example of a multi tracker configuration that runs slow but accurate
idlepage tracker with 20 second interval, and the fast softdirty
tracker with 5 second interval:

```
policy:
  name: ...
  config: |
    ...
    tracker:
      name: multi
      config: |
        trackers:
          - name: idlepage
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 20000
          - name: softdirty
            config: |
              pagesinregion: 512
              maxcountperregion: 1
              scanintervalms: 5000
```

## Routines

Routines are configured and executed independently of policies and
trackers.

### StatActions routine

The `statactions` routine executes commands based on configured
criteria. Each of these routines run periodically with the interval of
`intervalms` milliseconds, checking which criteria are fulfilled, and
then executing corresponding commands.

Following is an example of an memtierd configuration that configures
only a policy stub but has two routines. The first routine prints time
to `statactions-1s-period.txt` every second. The second routine checks
every 5 seconds whether or not more than 3000 MB of memory has been
paged out. If this is the case, the routine executes a command that
frees the page cache. Then the routine resets its internal counter to
start waiting for the next 3000 MB to be paged out.

```
policy:
  name: stub
routines:
- name: statactions
  config: |
    intervalms: 1000
    intervalcommand: ["sh", "-c", "date +%s.%N >> /tmp/statactions-1s-period.txt"]
- name: statactions
  config: |
    intervalms: 5000
    pageoutmb: 3000
    pageoutcommand: ["sh", "-c", "echo 1 > /proc/sys/vm/drop_caches"]
```

Configuration parameters:
- `intervalms` specifies statistics check interval in
  milliseconds. For instance, the amount of paged out memory is
  checked on this interval.
- `intervalcommand` specifies command to be executed on every
  `intervalms` even if no statistics intervals are matched.
- `intervalcommandrunner` specifies how the command is executed. Options are:
  - `exec`: fork and execute the command in the system without a
    shell. This is the default. Example: `intervalcommand: ["sh", "-c", "echo x"]`
  - `memtier`: run simple memtier command. Example: `intervalcommand: ["stats", "-t", "events"]`
  - `memtier-prompt`: run a single-string command as if it was written
    to interactive prompt. Allows piping memtier command output to
    shell. Example:
    `intervalcommand: ["stats -t process_madvise | awk '/[0-9]+/{print $6}' > paged_out.txt"]`
- `pageoutmb` specifies interval in megabytes of memory that has been
  paged out.
- `pageoutcommand` specifies command to be executed on every
  `pageoutmb` interval.
- `pageoutcommandrunner` specifies how `pageoutcommand` is executed,
  see `intervalcommandrunner` for options.
- `timestamp` specifies how to format timestamps that are printed just
  before running a command. The default is an empty string, that is,
  timestamp is not printed. The format string accepts templates as in
  Go [Time.Format](https://pkg.go.dev/time#Time.Format), and
  shorthands including
  - `unix.s`, `unix.milli`, `unix.micro`, `unix.nano` for printing
    Unix epoch
  - `duration.s`, `duration.milli`, `duration.micro`, `duration.nano`
    for printing seconds since starting the routine, and
  - `rfc822`, `rfc822z`, `rfc850`, `rfc1123`, `rfc3339`,
  `rfc3339nano`, for printing standard human readable timestamps.
- `timestampafter` is like `timestamp`, but specifies format for
  timestamps after running a command.

#!/bin/bash
# usage: kernel-download $version
kernel-download() {
    vm-command "[ -d linux ]" && vm-command "rm -rf linux"

    vm-command "apt-get update" || error "failed to refresh apt package DB"
    vm-install-pkg wget git-core

    local version=${1:-$linux}
    if [[ ! -z ${version} ]]; then
        url="https://git.kernel.org/pub/scm/linux/kernel/git/stable/linux.git/snapshot/linux-${version}.tar.gz"
        vm-command "wget -q -c ${url} -O - | tar -xz" || error "failed to download kernel"
        vm-command "mv linux-${version} linux"
    else
        vm-command "git clone https://github.com/torvalds/linux"
    fi
}

# usage: kernel-patch $version $patch_file
kernel-patch() {
    # download linux kernel source code
    if [[ ! -z $1 ]] || [[ ! -z $linux ]]; then
        kernel-download $1 || error "failed to download kernel"
    fi

    # patch kernel configuration
    if [[ ! -z $2 ]]; then
        cat $2 | vm-pipe-to-file linux/config.patch
        vm-command "cd linux && patch -p1 < ./config.patch" || {
            error "failed to patch configuration"
        }

        # compile kernel and generate the pkg
        kernel-build-and-install || error "failed to compile kernel"
    fi
}

kernel-build-and-install() {
    vm-command "[ -f linux-image-*.deb ]" && vm-command "rm -rf linux-image-*.deb"

    # install necessary pkgs for compiling kernel
    vm-command "apt-get update" || error "failed to refresh apt package DB"
    vm-install-pkg build-essential linux-source bc kmod cpio flex libncurses5-dev libelf-dev libssl-dev dwarves bison libtraceevent-dev libb2-1 libbabeltrace-dev libopencsd1 libpython3.11 libpython3.11-minimal libpython3.11-stdlib libunwind8 python3.11 python3.11-minimal debhelper systemtap-sdt-dev libunwind-dev libslang2-dev libperl-dev python-dev-is-python3 libiberty-dev liblzma-dev libcap-dev libnuma-dev pkg-config bpftrace

    # compile kernel and generate the pkg
    vm-command "cd linux; nice make -j8 bindeb-pkg" || {
        error "failed to build kernel packages"
    }

    # install the new pkg and reboot
    vm-command "dpkg -i $(ls linux-image-*.deb | grep -v dbg)"
    vm-reboot
}

kernel-configure-damon-idlepage() {
    if [ "$distro" != "debian-sid" ] && [ "$distro" != "ubuntu-22.04" ]; then
        error "kernel-configure-{damon,idlepage} is implemented only for distro=debian-sid and distro=ubuntu-22.04"
    fi

    # Clone the latest Linux kernel and setup kernel development environment
    kernel-download 6.7-rc3 || error "failed to download kernel"

    # read the existing .config file that is used for the current bootOS
    vm-command "cd linux && make olddefconfig" || error "failed to make olddefconfig"

    # enable kernel configuration options for DAMOM
    vm-command "echo 'CONFIG_DAMON=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_KUNIT_TEST=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_VADDR=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_PADDR=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_VADDR_KUNIT_TEST=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_SYSFS=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_DBGFS=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_DBGFS_KUNIT_TEST=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_RECLAIM=y' >>linux/.config"
    vm-command "echo 'CONFIG_DAMON_LRU_SORT=y' >>linux/.config"

    # enable kernel configuration options for IDLEPAGE, which only required by debian-sid
    # as IDLEPAGE is enabled by default on ubuntu-22.04
    if [ "$distro" == "debian-sid" ]; then
        vm-command "echo 'CONFIG_PAGE_IDLE_FLAG=y' >>linux/.config"
        vm-command "echo 'CONFIG_IDLE_PAGE_TRACKING=y' >>linux/.config"
    fi

    # compile kernel and generate the pkg
    kernel-build-and-install || error "failed to compile kernel"
}

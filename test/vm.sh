#!/bin/sh
# Start disposable QEMU virtual machines, run a command in each over SSH as a
# user with passwordless sudo, and destroy them.
#
#   test/vm.sh [distros] [command]
#
#   distros    quick  ubuntu (default)
#              pair   ubuntu and arch, the two most different
#              all    every distribution in image_url
#              or a comma-separated list: ubuntu,fedora
#   command    what to run; by default, identify the machine
#
# The command receives the machine name in UXSM_VM_DISTRO: ubuntu, fedora…
#
# UXSM_VM_COMMAND_TIMEOUT=seconds is the time allowed before considering the
# command hung; the default is half an hour.
# UXSM_VM_UPLOAD=dir copies dir to ~/uxsm on each machine before the command.
# UXSM_VM_DOWNLOAD=dir copies ~/uxsm/out from each machine to dir after success.
#
#   test/vm.sh all
#   test/vm.sh pair 'uname -r'
#
# Requires qemu-system-x86_64, qemu-img, cloud-localds (cloud-image-utils), ssh,
# and curl. It uses KVM when /dev/kvm is available; without it, VMs are very slow.
# Images are downloaded once under $XDG_CACHE_HOME/uxsm/vm

set -eu

CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/uxsm/vm
SSH_PORT=${UXSM_VM_SSH_PORT:-2222}
BOOT_TIMEOUT=${UXSM_VM_BOOT_TIMEOUT:-300}
# Time allowed for the command inside the VM before considering it hung.
COMMAND_TIMEOUT=${UXSM_VM_COMMAND_TIMEOUT:-1800}
VM_USER=uxsm

DEFAULT_COMMAND='. /etc/os-release; echo "$PRETTY_NAME, kernel $(uname -r)"; id; sudo -n true && echo "sudo: ok"; echo "system: $(systemctl is-system-running --wait)"'

# Official cloud images, all with cloud-init.
image_url() {
    case "$1" in
    ubuntu)   echo https://cloud-images.ubuntu.com/releases/noble/release/ubuntu-24.04-server-cloudimg-amd64.img ;;
    debian)   echo https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2 ;;
    arch)     echo https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2 ;;
    fedora)   echo https://download.fedoraproject.org/pub/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2 ;;
    opensuse) echo https://download.opensuse.org/tumbleweed/appliances/openSUSE-Tumbleweed-Minimal-VM.x86_64-Cloud.qcow2 ;;
    *)        return 1 ;;
    esac
}

case "${1:-quick}" in
quick) distros=ubuntu ;;
pair)  distros="ubuntu arch" ;;
all)   distros="ubuntu debian arch fedora opensuse" ;;
*)     distros=$(printf '%s' "$1" | tr ',' ' ') ;;
esac
command=${2:-$DEFAULT_COMMAND}

for d in $distros; do
    image_url "$d" >/dev/null || { echo "vm.sh: unknown distro: $d" >&2; exit 2; }
done
for tool in qemu-system-x86_64 qemu-img cloud-localds ssh ssh-keygen curl; do
    command -v "$tool" >/dev/null 2>&1 || { echo "vm.sh: $tool not found" >&2; exit 1; }
done

# With KVM, expose the real host CPU to the guest. This used to be "max", every
# feature QEMU can emulate, which made an Ubuntu guest crash its own kernel at
# boot ("Attempted to kill the idle task!"). Without KVM there is no real CPU
# to pass through, so "max" remains the fallback.
accel=tcg
cpu=max
if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then
    accel=kvm
    cpu=host
else
    echo "vm.sh: /dev/kvm not usable, falling back to emulation (slow)" >&2
fi

work=
cleanup() {
    if [ -n "$work" ]; then
        if [ -f "$work/qemu.pid" ]; then
            pid=$(cat "$work/qemu.pid")
            kill "$pid" 2>/dev/null || true
            # Wait for full termination; otherwise the next VM may find the SSH
            # port still occupied.
            while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
        fi
        rm -rf "$work"
    fi
    work=
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# vm_ssh [--timeout SECONDS] COMMAND…: run the command inside the VM.
#
# The limit is necessary because a hung VM—for example after a guest kernel
# panic—leaves the connection open and silent. Without it, the run waits forever
# for a reply that will never arrive.
vm_ssh() {
    limit=
    if [ "${1:-}" = --timeout ]; then
        limit="timeout $2"
        shift 2
    fi
    # shellcheck disable=SC2086
    $limit ssh -i "$work/key" -p "$SSH_PORT" \
        -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o LogLevel=ERROR -o ConnectTimeout=3 -o BatchMode=yes \
        "$VM_USER@127.0.0.1" "$@"
}

# run_vm DISTRO: start the VM, run $command, and destroy it.
run_vm() {
    distro=$1
    url=$(image_url "$distro")
    image=$CACHE/$(basename "$url")

    mkdir -p "$CACHE"
    if [ ! -f "$image" ]; then
        echo "vm.sh[$distro]: downloading $url" >&2
        curl -fsSL -o "$image.part" "$url"
        mv "$image.part" "$image"
    fi

    work=$(mktemp -d)

    # Disk: use a temporary overlay so the cached image is never modified.
    qemu-img create -q -f qcow2 -F qcow2 -b "$image" "$work/disk.qcow2" 10G

    # Access: pass a single-use key to cloud-init.
    ssh-keygen -q -t ed25519 -N '' -C uxsm-vm -f "$work/key"
    cat > "$work/user-data" <<EOF
#cloud-config
users:
  - name: $VM_USER
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - $(cat "$work/key.pub")
EOF
    printf 'instance-id: uxsm-vm-%s\nlocal-hostname: uxsm-%s\n' "$distro" "$distro" > "$work/meta-data"
    cloud-localds "$work/seed.img" "$work/user-data" "$work/meta-data"

    qemu-system-x86_64 \
        -machine q35,accel="$accel" -cpu "$cpu" -smp 2 -m 2048 \
        -drive file="$work/disk.qcow2",if=virtio,format=qcow2 \
        -drive file="$work/seed.img",if=virtio,format=raw \
        -netdev user,id=net0,hostfwd=tcp:127.0.0.1:"$SSH_PORT"-:22 \
        -device virtio-net-pci,netdev=net0 \
        -display none -serial file:"$work/console.log" \
        -daemonize -pidfile "$work/qemu.pid" || {
        echo "vm.sh[$distro]: qemu did not start" >&2
        return 1
    }

    start=$(date +%s)
    until vm_ssh true 2>/dev/null; do
        if [ $(( $(date +%s) - start )) -ge "$BOOT_TIMEOUT" ]; then
            echo "vm.sh[$distro]: no ssh after ${BOOT_TIMEOUT}s; last console lines:" >&2
            tail -n 20 "$work/console.log" >&2
            return 1
        fi
        sleep 2
    done
    echo "vm.sh[$distro]: ssh up after $(( $(date +%s) - start ))s" >&2

    # UXSM_VM_UPLOAD: a directory copied to ~/uxsm before the command.
    if [ -n "${UXSM_VM_UPLOAD:-}" ]; then
        tar -C "$UXSM_VM_UPLOAD" -cf - . | vm_ssh 'mkdir -p ~/uxsm && tar -C ~/uxsm -xf -'
    fi

    # Return the command's exit status, not sed's.
    { vm_ssh --timeout "$COMMAND_TIMEOUT" "export UXSM_VM_DISTRO=$distro; $command" 2>&1
      echo $? > "$work/status"
    } | sed -u "s/^/[$distro] /"
    rc=$(cat "$work/status")
    if [ "$rc" -ne 0 ]; then
        # timeout returns 124; when the VM has crashed, the cause is on the
        # console rather than in command output.
        if [ "$rc" -eq 124 ]; then
            echo "vm.sh[$distro]: the command did not finish in ${COMMAND_TIMEOUT}s" >&2
        fi
        if grep -q "Kernel panic" "$work/console.log" 2>/dev/null; then
            echo "vm.sh[$distro]: the guest kernel panicked; last console lines:" >&2
            tail -n 20 "$work/console.log" >&2
        fi
        return "$rc"
    fi

    # UXSM_VM_DOWNLOAD retrieves what the command left in the ~/uxsm/out directory.
    if [ -n "${UXSM_VM_DOWNLOAD:-}" ]; then
        mkdir -p "$UXSM_VM_DOWNLOAD"
        vm_ssh 'tar -C ~/uxsm/out -cf - .' | tar -C "$UXSM_VM_DOWNLOAD" -xf - || {
            echo "vm.sh[$distro]: could not download ~/uxsm/out" >&2
            return 1
        }
    fi
}

summary=
status=0
for d in $distros; do
    if run_vm "$d"; then
        summary="$summary$d: ok\n"
    else
        summary="$summary$d: FAILED\n"
        status=1
    fi
    cleanup
done

printf '\n%b' "$summary"
exit "$status"

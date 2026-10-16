#!/bin/sh
# Start disposable QEMU virtual machines, run a command in each over SSH as a
# user with passwordless sudo, and destroy them.
#
# Each machine starts from a prepared image of its distribution, kept in the
# cache: the official cloud image, updated, with the test dependencies
# installed and LightDM downloaded (test/prepare.sh). A machine writes to a
# temporary layer over it, which is thrown away afterwards, so the build VM and
# the clean test VM of release.sh start from the same untouched image and
# neither has to download what the other already did. test/vm.yaml says when
# the image is updated (refresh) and when it is made again from a cloud image
# downloaded anew (lifetime); a failed update also makes it again.
# UXSM_VM_FRESH=1 skips it and starts from the cloud image, to rule the image
# out when something fails. Freeing the disk is a separate matter, for
# test/vm-cleanup.sh.
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
# UXSM_MIRROR_WAITS, if set, is passed on to the command (see test/mirror.sh).
# UXSM_VM_LOGS=dir is where a failed machine leaves its console and journal; by
# default, build/vm/logs in the repository.
# UXSM_VM_REFRESH, UXSM_VM_LIFETIME: override test/vm.yaml for this run.
#
#   test/vm.sh all
#   test/vm.sh pair 'uname -r'
#
# Requires qemu-system-x86_64, qemu-img, cloud-localds (cloud-image-utils), ssh,
# and curl. It uses KVM when /dev/kvm is available; without it, VMs are very slow.
# Images live under $XDG_CACHE_HOME/uxsm/vm, with the SSH key of the machines:
# it is the same for every run, because a prepared image only lets in the key
# it was made with. The machines listen only on 127.0.0.1.

set -eu
. "$(dirname "$0")/vm-config.sh"

CACHE=$VM_CACHE
SSH_PORT=${UXSM_VM_SSH_PORT:-2222}
BOOT_TIMEOUT=${UXSM_VM_BOOT_TIMEOUT:-300}
# Time allowed for the command inside the VM before considering it hung.
COMMAND_TIMEOUT=${UXSM_VM_COMMAND_TIMEOUT:-1800}
LOGS=${UXSM_VM_LOGS:-$(cd "$(dirname "$0")/.." && pwd)/build/vm/logs}
KEY=$CACHE/key
PREPARED=$CACHE/prepared
HERE=$(cd "$(dirname "$0")" && pwd)
VM_USER=uxsm

DEFAULT_COMMAND='. /etc/os-release; echo "$PRETTY_NAME, kernel $(uname -r)"; id; sudo -n true && echo "sudo: ok"; echo "system: $(systemctl is-system-running --wait)"'

# Official cloud images, all with cloud-init.
image_url() {
  case "$1" in
  ubuntu) echo https://cloud-images.ubuntu.com/releases/noble/release/ubuntu-24.04-server-cloudimg-amd64.img ;;
  debian) echo https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2 ;;
  arch) echo https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2 ;;
  fedora) echo https://download.fedoraproject.org/pub/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2 ;;
  opensuse) echo https://download.opensuse.org/tumbleweed/appliances/openSUSE-Tumbleweed-Minimal-VM.x86_64-Cloud.qcow2 ;;
  *) return 1 ;;
  esac
}

REFRESH=$(setting refresh) && REFRESH=$(duration refresh "$REFRESH") || exit 2
LIFETIME=$(setting lifetime) && LIFETIME=$(duration lifetime "$LIFETIME") || exit 2

case "${1:-quick}" in
quick) distros=ubuntu ;;
pair) distros="ubuntu arch" ;;
all) distros="ubuntu debian arch fedora opensuse" ;;
*) distros=$(printf '%s' "$1" | tr ',' ' ') ;;
esac
command=${2:-$DEFAULT_COMMAND}

for d in $distros; do
  image_url "$d" >/dev/null || {
    echo "vm.sh: unknown distro: $d" >&2
    exit 2
  }
done
for tool in qemu-system-x86_64 qemu-img cloud-localds ssh ssh-keygen curl flock; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "vm.sh: $tool not found" >&2
    exit 1
  }
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
  $limit ssh -i "$KEY" -p "$SSH_PORT" \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR -o ConnectTimeout=3 -o BatchMode=yes \
    "$VM_USER@127.0.0.1" "$@"
}

# save_logs: keep the console and, if ssh still answers, the journal of a failed
# machine. cleanup destroys the machine, and with it the only evidence of an
# intermittent failure that a second run may not reproduce.
save_logs() {
  dir=$LOGS/$distro-$(date +%H%M%S)
  mkdir -p "$dir"
  cp "$work/console.log" "$dir/" 2>/dev/null || true
  if ! vm_ssh --timeout 60 'sudo journalctl -b --no-pager -o short-monotonic' >"$dir/journal.txt" 2>/dev/null; then
    rm -f "$dir/journal.txt"
    echo "vm.sh[$distro]: no journal, ssh does not answer" >&2
  fi
  echo "vm.sh[$distro]: logs in $dir" >&2
}

# fetch_base DISTRO [fresh]: set $image to the distribution's cloud image,
# downloading it if missing or, with fresh, if older than LIFETIME.
fetch_base() {
  url=$(image_url "$1")
  image=$CACHE/$(basename "$url")
  if [ -f "$image" ] && { [ "${2:-}" != fresh ] ||
    [ $(($(date +%s) - $(stat -c %Y "$image"))) -lt "$LIFETIME" ]; }; then
    return
  fi
  echo "vm.sh[$1]: downloading $url" >&2
  curl -fsSL -o "$image.part" "$url"
  mv "$image.part" "$image"
}

# start_vm DISK: boot DISK in $work, with cloud-init's seed, and wait for ssh.
start_vm() {
  cat >"$work/user-data" <<EOF
#cloud-config
users:
  - name: $VM_USER
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - $(cat "$KEY.pub")
EOF
  # The instance ID never changes, so a prepared image does not run
  # cloud-init's first boot again.
  printf 'instance-id: uxsm-vm-%s\nlocal-hostname: uxsm-%s\n' "$distro" "$distro" >"$work/meta-data"
  cloud-localds "$work/seed.img" "$work/user-data" "$work/meta-data"

  qemu-system-x86_64 \
    -machine q35,accel="$accel" -cpu "$cpu" -smp 2 -m 2048 \
    -drive file="$1",if=virtio,format=qcow2 \
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
    if [ $(($(date +%s) - start)) -ge "$BOOT_TIMEOUT" ]; then
      echo "vm.sh[$distro]: no ssh after ${BOOT_TIMEOUT}s; last console lines:" >&2
      tail -n 20 "$work/console.log" >&2
      save_logs
      return 1
    fi
    sleep 2
  done
  echo "vm.sh[$distro]: ssh up after $(($(date +%s) - start))s" >&2
}

# stop_vm: power the machine off cleanly, so that its disk can be kept.
stop_vm() {
  vm_ssh 'sudo systemctl poweroff' 2>/dev/null || true
  pid=$(cat "$work/qemu.pid")
  i=0
  while kill -0 "$pid" 2>/dev/null; do
    i=$((i + 1))
    [ "$i" -lt 600 ] || {
      echo "vm.sh[$distro]: the machine did not power off in 2 minutes" >&2
      return 1
    }
    sleep 0.2
  done
  rm -f "$work/qemu.pid"
}

# run_prepare DISK: boot DISK, run test/prepare.sh on it, and power it off.
#
# prepare.sh runs as a transient unit and is waited for by polling, not over
# the ssh session: an upgrade may restart sshd, and a session waiting on it
# would hang until the timeout.
run_prepare() {
  work=$(mktemp -d)
  mkdir "$work/prepare"
  cp "$HERE/prepare.sh" "$HERE/mirror.sh" "$work/prepare/"
  printf 'sh /var/tmp/uxsm-prepare/prepare.sh >/var/tmp/uxsm-prepare/log 2>&1\necho $? >/var/tmp/uxsm-prepare/status\n' \
    >"$work/prepare/run.sh"
  start_vm "$1" || {
    cleanup
    return 1
  }
  tar -C "$work/prepare" -cf - . | vm_ssh 'mkdir -p /var/tmp/uxsm-prepare && tar -C /var/tmp/uxsm-prepare -xf -'
  vm_ssh "sudo systemd-run --quiet --collect --unit=uxsm-prepare ${UXSM_MIRROR_WAITS:+-E UXSM_MIRROR_WAITS='$UXSM_MIRROR_WAITS' }sh /var/tmp/uxsm-prepare/run.sh"
  begin=$(date +%s)
  until vm_ssh 'test -f /var/tmp/uxsm-prepare/status' 2>/dev/null; do
    if [ $(($(date +%s) - begin)) -ge "$COMMAND_TIMEOUT" ]; then
      echo "vm.sh[$distro]: preparing the image did not finish in ${COMMAND_TIMEOUT}s" >&2
      break
    fi
    sleep 5
  done
  vm_ssh 'cat /var/tmp/uxsm-prepare/log' 2>/dev/null | sed "s/^/[$distro] prepare: /"
  prc=$(vm_ssh 'cat /var/tmp/uxsm-prepare/status' 2>/dev/null || echo 124)
  if [ "$prc" -eq 0 ]; then
    { vm_ssh 'sudo rm -rf /var/tmp/uxsm-prepare' && stop_vm; } || prc=1
  else
    save_logs
  fi
  cleanup
  return "$prc"
}

# prepare DISTRO: set $prepared to the distribution's prepared image, making or
# updating it first if it is due; see the header.
prepare() {
  d=$1
  prepared=$PREPARED/$d.qcow2
  update=$PREPARED/$d.update.qcow2
  new=$PREPARED/$d.new.qcow2
  mkdir -p "$PREPARED"
  now=$(date +%s)
  built=$(cat "$PREPARED/$d.built" 2>/dev/null || echo 0)
  updated=$(cat "$PREPARED/$d.updated" 2>/dev/null || echo 0)
  if [ -f "$prepared" ] && [ $((now - built)) -lt "$LIFETIME" ]; then
    if [ $((now - updated)) -lt "$REFRESH" ]; then
      echo "vm.sh[$d]: prepared image updated $(((now - updated) / 60)) min ago" >&2
      return
    fi
    echo "vm.sh[$d]: updating the prepared image" >&2
    qemu-img create -q -f qcow2 -F qcow2 -b "$prepared" "$update"
    prc=0
    run_prepare "$update" || prc=$?
    if [ "$prc" -eq 0 ]; then
      qemu-img commit -q "$update"
      rm -f "${update:?}"
      date +%s >"$PREPARED/$d.updated"
      return
    fi
    rm -f "${update:?}"
    # A broken mirror leaves the image as it was, still usable; anything else
    # may have left it half updated, so it is made again.
    if [ "$prc" -eq 75 ]; then
      echo "vm.sh[$d]: the mirror is broken; using the prepared image as it was" >&2
      return
    fi
    echo "vm.sh[$d]: updating the prepared image failed; making it again" >&2
  fi

  fetch_base "$d" fresh
  echo "vm.sh[$d]: making the prepared image" >&2
  rm -f "${new:?}"
  qemu-img convert -O qcow2 "$image" "$new"
  qemu-img resize -q "$new" 10G
  prc=0
  run_prepare "$new" || prc=$?
  if [ "$prc" -ne 0 ]; then
    rm -f "${new:?}"
    return "$prc"
  fi
  mv "$new" "$prepared"
  date +%s >"$PREPARED/$d.built"
  date +%s >"$PREPARED/$d.updated"
}

# run_vm DISTRO: start the VM, run $command, and destroy it.
run_vm() {
  distro=$1
  mkdir -p "$CACHE"

  if [ -n "${UXSM_VM_FRESH:-}" ]; then
    fetch_base "$distro"
    backing=$image
  else
    prepare "$distro" || return
    backing=$prepared
    # When it was last used, for test/vm-cleanup.sh.
    touch "$PREPARED/$distro.used"
  fi

  work=$(mktemp -d)
  # Disk: a temporary layer, so the image under it is never modified.
  qemu-img create -q -f qcow2 -F qcow2 -b "$backing" "$work/disk.qcow2" 10G
  start_vm "$work/disk.qcow2" || return

  # UXSM_VM_UPLOAD: a directory copied to ~/uxsm before the command.
  if [ -n "${UXSM_VM_UPLOAD:-}" ]; then
    tar -C "$UXSM_VM_UPLOAD" -cf - . | vm_ssh 'mkdir -p ~/uxsm && tar -C ~/uxsm -xf -'
  fi

  # Return the command's exit status, not sed's.
  {
    vm_ssh --timeout "$COMMAND_TIMEOUT" "export UXSM_VM_DISTRO=$distro${UXSM_MIRROR_WAITS:+ UXSM_MIRROR_WAITS='$UXSM_MIRROR_WAITS'}; $command" 2>&1
    echo $? >"$work/status"
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
    save_logs
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

mkdir -p "$CACHE"
# Shared, so that runs do not wait for each other; test/vm-cleanup.sh takes it
# alone, and so never deletes an image under a machine. QEMU inherits it, which
# keeps it held for as long as a machine is up.
exec 9>>"$CACHE/lock"
flock -s 9

# A new key cannot open the prepared images made with the old one, so they go
# with it.
if [ ! -f "$KEY" ]; then
  ssh-keygen -q -t ed25519 -N '' -C uxsm-vm -f "$KEY"
  rm -rf "${PREPARED:?}"
fi

# A command that exits with 75 could not download from the distribution
# (test/mirror.sh): MIRROR, not FAILED, and vm.sh exits with 75 only when that
# is all that went wrong, so callers can tell the two apart.
summary=
status=0
for d in $distros; do
  rc=0
  run_vm "$d" || rc=$?
  if [ "$rc" -eq 0 ]; then
    summary="$summary$d: ok\n"
  elif [ "$rc" -eq 75 ]; then
    summary="$summary$d: MIRROR\n"
    [ "$status" -ne 0 ] || status=75
  else
    summary="$summary$d: FAILED\n"
    status=1
  fi
  cleanup
done

printf '\n%b' "$summary"
exit "$status"

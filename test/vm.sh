#!/bin/sh
# Arranca máquinas virtuales desechables con QEMU, ejecuta una orden en cada una
# por ssh, como un usuario con sudo sin contraseña, y las destruye.
#
#   test/vm.sh [distros] [orden]
#
#   distros    quick  ubuntu (por defecto)
#              pair   ubuntu y arch, las dos más distintas
#              all    todas las de image_url
#              o una lista separada por comas: ubuntu,fedora
#   orden      lo que se ejecuta; por defecto, qué máquina es
#
# La orden recibe en UXSM_VM_DISTRO el nombre de la máquina: ubuntu, fedora…
#
# UXSM_VM_COMMAND_TIMEOUT=segundos es lo que se le da a la orden antes de darla
# por colgada; por defecto, media hora.
# UXSM_VM_UPLOAD=dir copia dir a ~/uxsm en cada máquina antes de la orden.
# UXSM_VM_DOWNLOAD=dir trae ~/uxsm/out de cada máquina a dir si la orden termina
# bien.
#
#   test/vm.sh all
#   test/vm.sh pair 'uname -r'
#
# Necesita qemu-system-x86_64, qemu-img, cloud-localds (cloud-image-utils), ssh y
# curl. Usa KVM si /dev/kvm se puede usar; sin él, cada máquina va muy lenta.
# Las imágenes se descargan una vez a $XDG_CACHE_HOME/uxsm/vm.

set -eu

CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/uxsm/vm
SSH_PORT=${UXSM_VM_SSH_PORT:-2222}
BOOT_TIMEOUT=${UXSM_VM_BOOT_TIMEOUT:-300}
# Lo que se le da a la orden dentro de la máquina antes de darla por colgada.
COMMAND_TIMEOUT=${UXSM_VM_COMMAND_TIMEOUT:-1800}
VM_USER=uxsm

DEFAULT_COMMAND='. /etc/os-release; echo "$PRETTY_NAME, kernel $(uname -r)"; id; sudo -n true && echo "sudo: ok"; echo "system: $(systemctl is-system-running --wait)"'

# Imágenes «cloud» oficiales, todas con cloud-init.
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

# Con KVM, el procesador que ve la máquina es el de verdad. Antes era "max", que
# es todo lo que QEMU sabe emular, y con él una máquina de Ubuntu se llevó por
# delante su propio núcleo nada más arrancar ("Attempted to kill the idle
# task!"). Sin KVM no hay procesador real que pasar, así que queda "max".
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
            # Esperar a que termine de verdad: si no, la máquina siguiente
            # puede encontrarse el puerto de ssh todavía ocupado.
            while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
        fi
        rm -rf "$work"
    fi
    work=
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# vm_ssh [--timeout SEGUNDOS] ORDEN…: ejecuta la orden dentro de la máquina.
#
# El límite hace falta porque una máquina que se cuelga ―un kernel panic del
# invitado, por ejemplo― deja la conexión abierta y callada: sin él, la tanda se
# queda esperando una respuesta que no va a llegar nunca.
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

# run_vm DISTRO: arranca la máquina, ejecuta $command y la destruye.
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

    # Disco: una capa temporal encima, para no modificar nunca la imagen guardada.
    qemu-img create -q -f qcow2 -F qcow2 -b "$image" "$work/disk.qcow2" 10G

    # Acceso: una clave de un solo uso, que se le pasa a cloud-init.
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

    # UXSM_VM_UPLOAD: un directorio que se copia a ~/uxsm antes de la orden.
    if [ -n "${UXSM_VM_UPLOAD:-}" ]; then
        tar -C "$UXSM_VM_UPLOAD" -cf - . | vm_ssh 'mkdir -p ~/uxsm && tar -C ~/uxsm -xf -'
    fi

    # El código de salida de la orden, no el de sed.
    { vm_ssh --timeout "$COMMAND_TIMEOUT" "export UXSM_VM_DISTRO=$distro; $command" 2>&1
      echo $? > "$work/status"
    } | sed -u "s/^/[$distro] /"
    rc=$(cat "$work/status")
    if [ "$rc" -ne 0 ]; then
        # 124 es lo que devuelve timeout; y con la máquina caída, la causa está
        # en la consola, no en la salida de la orden.
        if [ "$rc" -eq 124 ]; then
            echo "vm.sh[$distro]: the command did not finish in ${COMMAND_TIMEOUT}s" >&2
        fi
        if grep -q "Kernel panic" "$work/console.log" 2>/dev/null; then
            echo "vm.sh[$distro]: the guest kernel panicked; last console lines:" >&2
            tail -n 20 "$work/console.log" >&2
        fi
        return "$rc"
    fi

    # UXSM_VM_DOWNLOAD: lo que la orden haya dejado en ~/uxsm/out.
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

#!/bin/sh
# Delete the VM images of test/vm.sh that nothing has used for `cleanup` in
# test/vm.yaml, or, with `cleanup: boot`, those not used since the computer
# started. The uxsm-vm-cleanup timer runs it once a day (`make vm-cleanup`);
# it can also be run by hand.
#
# A prepared image was last used when vm.sh last started a machine from it,
# which it records in <distro>.used; a cloud image, when it was downloaded,
# since it is only read to make a prepared image right after. The SSH key
# stays: it takes nothing, and a new one would lock every run out of the
# prepared images that are left.

set -eu
. "$(dirname "$0")/vm-config.sh"

cleanup=$(setting cleanup) || exit 2
if [ "$cleanup" = boot ]; then
  limit=$(sed -n 's/^btime //p' /proc/stat)
else
  age=$(duration cleanup "$cleanup") || exit 2
  limit=$(($(date +%s) - age))
fi
[ -d "$VM_CACHE" ] || exit 0

# vm.sh holds this lock while it runs, so an image is never deleted under a
# machine; a busy cache is left for the next day.
exec 9>>"$VM_CACHE/lock"
flock -n 9 || {
  echo "vm-cleanup.sh: a VM is running; nothing deleted"
  exit 0
}

for used in "$VM_CACHE"/prepared/*.used "$VM_CACHE"/prepared/*.qcow2; do
  [ -f "$used" ] || continue
  d=${used%.*}
  # An image without a .used file, made before vm.sh wrote one, counts from
  # its last change.
  [ "$used" = "$d.qcow2" ] && [ -f "$d.used" ] && continue
  if [ "$(stat -c %Y "$used")" -lt "$limit" ]; then
    echo "vm-cleanup.sh: deleting the prepared image $d.qcow2"
    rm -f "$d".*
  fi
done
for image in "$VM_CACHE"/*.img "$VM_CACHE"/*.qcow2; do
  [ -f "$image" ] || continue
  if [ "$(stat -c %Y "$image")" -lt "$limit" ]; then
    echo "vm-cleanup.sh: deleting the cloud image $image"
    rm -f "$image"
  fi
done

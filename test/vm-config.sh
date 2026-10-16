# Shared by test/vm.sh and test/vm-cleanup.sh, which source it: where the VM
# images live and how to read test/vm.yaml.

VM_CACHE=${XDG_CACHE_HOME:-$HOME/.cache}/uxsm/vm
VM_CONFIG=$(dirname "$0")/vm.yaml

# setting NAME: UXSM_VM_<NAME> if set, else NAME in test/vm.yaml. A setting
# missing from both is an error, so that the defaults live in one place.
setting() {
  var=UXSM_VM_$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')
  eval "val=\${$var:-}"
  if [ -z "$val" ]; then
    val=$(sed -n "s/^$1:[[:space:]]*\([^#[:space:]]*\).*/\1/p" "$VM_CONFIG" | tail -n 1)
  fi
  if [ -z "$val" ]; then
    echo "$(basename "$0"): $1 is not set in $VM_CONFIG" >&2
    return 2
  fi
  printf '%s\n' "$val"
}

# duration NAME VALUE: VALUE in seconds; NAME is the setting it came from.
duration() {
  n=${2%[dhm]}
  case "$n" in
  '' | *[!0-9]*) ;;
  *)
    case "$2" in
    *d) echo $((n * 86400)) && return ;;
    *h) echo $((n * 3600)) && return ;;
    *m) echo $((n * 60)) && return ;;
    esac
    ;;
  esac
  echo "$(basename "$0"): $1=$2 is not a duration such as 7d, 8h, or 30m" >&2
  return 2
}

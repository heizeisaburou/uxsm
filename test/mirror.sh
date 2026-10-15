# Retries for the commands that download from a distribution's repositories,
# loaded with `. ~/uxsm/mirror.sh` by the scripts that run inside the VMs.
#
# A rolling distribution's mirror can be caught halfway through a sync: on
# 2026-10-15 openSUSE's index named a file that was not there yet, and zypper
# refused the whole repository. That is not a uxsm failure, and it usually
# heals in minutes, so such a command is retried in the same VM; if it still
# fails, the script exits with 75 (EX_TEMPFAIL), which vm.sh and release.sh
# report as MIRROR instead of FAILED.
#
# Only downloads whose package list is ours to fix go through mirror: index
# refreshes, system updates, and the fixed lists of build and test tools.
# Whatever installs what uxsm's own recipe asks for (Build-Depends,
# BuildRequires, makepkg -s, the uxsm package and its Depends) does not, because
# there a failure may be uxsm's, and a retry would hide it behind a MIRROR.

# UXSM_MIRROR_WAITS: seconds to wait before each retry. The default adds up to
# a quarter of an hour; tests set a few seconds.
mirror_waits=${UXSM_MIRROR_WAITS:-60 120 240 480}

# mirror COMMAND...: run COMMAND, retrying after each wait while it fails.
mirror() {
  for mirror_wait in $mirror_waits; do
    "$@" && return 0
    echo "mirror: '$*' failed, retrying in ${mirror_wait}s" >&2
    sleep "$mirror_wait"
  done
  "$@" && return 0
  echo "mirror: '$*' still fails; the distribution's repositories are not usable" >&2
  exit 75
}

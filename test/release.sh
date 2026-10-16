#!/bin/sh
# Build uxsm packages and test them after installation, in disposable virtual
# machines through the test/vm.sh runner, as many distributions at a time as
# jobs in test/vm.yaml says. Each distribution is a run of its own:
#
#   1. Build its package in a VM of that distribution with its packaging tool,
#      under build/release/<distro>, unless the package there was built from
#      the same sources.
#   2. On a clean VM, install the package through the package manager and run
#      the test/integration/run.sh suite.
#
# NixOS is a run too: `make test-nixos`, a machine declared by the flake.
#
#   test/release.sh [distros]
#   test/release.sh --publish
#   test/release.sh --list [distros]
#
#   distros    quick  ubuntu (default)
#              pair   ubuntu and arch, the two most different
#              all    ubuntu, debian, arch, fedora, and opensuse
#              or a comma-separated list, which may name nixos: nixos,arch
#
# --list prints the distributions those names stand for, one per line, and
# runs nothing: CI starts one job for each, and this way only this script
# knows what quick, pair, and all mean.
#
# Runs stop at the first failure: a release needs all of them, so going on would
# only spend time on runs that have to be repeated anyway.
#
# Keys. A run that passes leaves a key in build/keys, valid while the files it
# tested stay the same and for 24 hours: Arch and Tumbleweed change daily, and
# a key from last week proves nothing about today's distribution. One run's key
# never depends on another's, so a failed run is repeated alone. --publish runs
# every distribution without a valid key and, once all six have one, promotes
# build/release to releases/latest, which is what the pre-push hook checks.
#
# Packages. A package is rebuilt only when what goes into it changes, so the
# installation and the tests can be adjusted without building from scratch.
# Its version carries the commit hash, so a new commit rebuilds it.
#
# Logs. Each run writes its complete output to build/logs/<distro>/<time>, with
# the time of each step and how long each took; the terminal shows only the
# steps, failures, and a summary. The ten latest runs of each distribution are
# kept, and nothing there is uploaded anywhere.
#
# With FAST=1, the package is built and tested in the same VM, saving one boot
# and one dependency installation. This loses the clean-machine guarantee: a
# clean VM has only the package and test requirements, exposing undeclared
# runtime dependencies. So a FAST run leaves no key, and --publish refuses it.
#
# Each distribution has its own package even when formats match: Debian and
# Ubuntu build against their own libraries and tools, as do Fedora and openSUSE.

set -eu
cd "$(dirname "$0")/.."
. test/vm-config.sh

jobs=$(setting jobs)
case "$jobs" in
'' | *[!0-9]* | 0)
  echo "release.sh: jobs=$jobs is not a number of runs such as 2" >&2
  exit 2
  ;;
esac

fast=${FAST:-}
publish=
list=
if [ "${1:-}" = --list ]; then
  list=1
  shift
fi
if [ "${1:-}" = --publish ]; then
  publish=1
  shift
  [ $# -eq 0 ] || {
    echo "release.sh: --publish always tests every distro" >&2
    exit 2
  }
  [ -z "$fast" ] || {
    echo "release.sh: --publish always builds and tests on separate machines" >&2
    exit 2
  }
  # NixOS goes first: it is the one that catches what the other five cannot,
  # since nothing there sits where every other distribution puts it, and it
  # takes under a minute. Then the slowest first (measured on 2026-10-16), so
  # that with runs side by side the last one to start is a short one.
  set -- nixos,debian,arch,fedora,ubuntu,opensuse
fi

case "${1:-quick}" in
quick) distros=ubuntu ;;
pair) distros="ubuntu arch" ;;
all) distros="ubuntu debian arch fedora opensuse" ;;
*) distros=$(printf '%s' "$1" | tr ',' ' ') ;;
esac

# recipe DISTRO: the test/package script that builds its package.
recipe() {
  case "$1" in
  ubuntu | debian) echo deb ;;
  arch) echo arch ;;
  fedora | opensuse) echo rpm ;;
  *) return 1 ;;
  esac
}

for d in $distros; do
  [ "$d" = nixos ] || recipe "$d" >/dev/null || {
    echo "release.sh: no package for $d yet" >&2
    exit 2
  }
done

if [ -n "$list" ]; then
  printf '%s\n' $distros
  exit 0
fi

# Package version comes from the test/version.sh script.
version=$(test/version.sh)

out=build/release
keys=build/keys
logs=build/logs
key_ttl=$((24 * 60 * 60))
rm -rf build/vm
mkdir -p "$out" "$keys" "$logs" build/vm
echo "$version" >"$out/version"

# sources: files known to git or not ignored by it, excluding deleted files.
sources() {
  git ls-files --cached --others --exclude-standard | while IFS= read -r f; do
    if [ -e "$f" ]; then printf '%s\n' "$f"; fi
  done | LC_ALL=C sort
}

# The tarball is byte for byte the same while the files are: sorted, with fixed
# owners and the commit's time on every file. So its checksum is the
# fingerprint of everything a run tests.
tarball=uxsm-$version.tar.gz
rm -f "$out"/uxsm-*.tar.gz
sources |
  tar --create --file=- --files-from=- --transform="s|^|uxsm-$version/|" \
    --owner=0 --group=0 --numeric-owner --mtime="@$(git log -1 --format=%ct)" |
  gzip -n >"$out/$tarball"
tested=$(sha256sum "$out/$tarball" | cut -d' ' -f1)

# The fingerprint of a package leaves out what cannot change it: the
# integration tests, the VM and release scripts, documentation (every .md
# file, wherever it is), CI, and the flake. Anything else counts, so a file
# added later rebuilds the packages rather than being missed.
packaged=$(sources |
  grep -vE '^(test/(integration|nixos|xsessions)/|test/(vm|vm-cleanup|vm-config|prepare|release|publish|xsessions)\.sh$|test/vm\.yaml$|test/uxsm-vm-cleanup\.|docs/|\.github/|\.githooks/|flake\.|.*\.md$|\.editorconfig$|\.gitignore$)' |
  xargs -d '\n' sha256sum | sha256sum | cut -d' ' -f1)

# has_package DISTRO: its package was built from these sources.
has_package() {
  [ "$(cat "$keys/$1.package" 2>/dev/null)" = "$version $packaged" ] &&
    [ -n "$(find "$out/$1" -type f 2>/dev/null)" ]
}

# has_passed DISTRO: its key is valid; prints when it was made.
has_passed() {
  [ -f "$keys/$1.passed" ] || return 1
  read -r key_version key_tested key_time <"$keys/$1.passed"
  [ "$key_version" = "$version" ] && [ "$key_tested" = "$tested" ] &&
    [ $(($(date +%s) - key_time)) -lt "$key_ttl" ] || return 1
  date -d "@$key_time" +%H:%M
}

# note TEXT: a step of the run, timed, in the log and on the terminal.
note() {
  line="$(date +%T) [$d] == $*"
  printf '%s\n' "$line" >>"$log"
  printf '%s\n' "$line" >&2
}

# stamp: copy a run's output to its log, timing the lines that mark a step or a
# failure and showing only those on the terminal.
stamp() {
  while IFS= read -r line || [ -n "$line" ]; do
    case $line in
    *"] == "* | vm.sh* | *"  FAIL: "* | *"  late: "* | *"  stuck: "* | \
      *"  skipped: "* | *"mirror: "* | *": ok" | *": FAILED" | *": MIRROR" | *"error: "*)
      line="$(date +%T) $line"
      printf '%s\n' "$line" >&2
      ;;
    esac
    printf '%s\n' "$line"
  done >>"$log"
}

# logged COMMAND...: run COMMAND through stamp and return its status.
logged() {
  {
    "$@" 2>&1
    echo $? >"$logdir/status"
  } | stamp
  set -- "$(cat "$logdir/status")"
  rm -f "$logdir/status"
  return "$1"
}

# durations LOG: how long each step took, from one timed line to the next.
durations() {
  awk '
    function secs(t, a) { split(t, a, ":"); return a[1] * 3600 + a[2] * 60 + a[3] }
    /^[0-9][0-9]:[0-9][0-9]:[0-9][0-9] / {
      t = secs($1)
      if (n++) {
        d = t - prev
        if (d < 0) d += 86400
        printf "%4d:%02d  %s\n", d / 60, d % 60, label
      }
      prev = t
      label = substr($0, 10)
      if (n == 1) first = t
    }
    END {
      d = prev - first
      if (d < 0) d += 86400
      printf "%4d:%02d  total\n", d / 60, d % 60
    }' "$1"
}

# run DISTRO: build its package if needed and test it on a clean VM. NixOS
# builds and boots its own machine.
run() {
  d=$1
  if [ "$d" = nixos ]; then
    note "NixOS machine"
    logged make --no-print-directory test-nixos || return
    echo "$version $tested $(date +%s)" >"$keys/$d.passed"
    return
  fi

  stage=build/vm/$d
  mkdir -p "$stage"
  if has_package "$d"; then
    note "the $d package was built from these sources; not rebuilding it"
  else
    rm -rf "${out:?}/$d" "$keys/$d.package"
    mkdir -p "$stage/build"
    cp "$out/version" "$out/$tarball" test/mirror.sh "$stage/build/"
    cp "test/package/$(recipe "$d").sh" "$stage/build/package.sh"
    # With FAST, the same VM builds the package and runs tests with it
    # installed: package.sh leaves it in ~/uxsm/out, where run.sh finds it and
    # vm.sh downloads it afterwards.
    if [ -n "$fast" ]; then
      cp -r test/integration "$stage/build/integration"
      note "build and test VM"
      UXSM_VM_UPLOAD=$stage/build UXSM_VM_DOWNLOAD=$out/$d UXSM_VM_LOGS=$logdir \
        logged test/vm.sh "$d" "sh ~/uxsm/package.sh &&
          mkdir -p ~/uxsm/packages &&
          cp -r ~/uxsm/out ~/uxsm/packages/$d &&
          sh ~/uxsm/integration/run.sh" || return
      echo "$version $packaged" >"$keys/$d.package"
      return
    fi
    note "build VM"
    # From the build image, with the build dependencies already there; the
    # recipe still installs them, so a new one is never missed.
    UXSM_VM_UPLOAD=$stage/build UXSM_VM_DOWNLOAD=$out/$d UXSM_VM_LOGS=$logdir \
      UXSM_VM_LAYER='sh ~/uxsm/package.sh deps' \
      logged test/vm.sh "$d" 'sh ~/uxsm/package.sh' || return
    echo "$version $packaged" >"$keys/$d.package"
  fi

  mkdir -p "$stage/test/packages"
  cp -r test/integration "$stage/test/integration"
  cp "$out/version" test/mirror.sh "$stage/test/"
  cp -r "$out/$d" "$stage/test/packages/$d"
  note "test VM"
  UXSM_VM_UPLOAD=$stage/test UXSM_VM_LOGS=$logdir \
    logged test/vm.sh "$d" 'sh ~/uxsm/integration/run.sh' || return
  echo "$version $tested $(date +%s)" >"$keys/$d.passed"
}

if [ -n "$fast" ]; then
  echo "== uxsm $version on $distros, building and testing on one machine each" >&2
else
  echo "== uxsm $version on $distros" >&2
fi

# one DISTRO: its run, with its log, its times, and its result in
# $results/DISTRO, printed as one block so that runs side by side do not mix
# their summaries.
one() {
  d=$1
  logdir=$logs/$d/$(date +%Y%m%d-%H%M%S)
  mkdir -p "$logdir"
  log=$logdir/run.log
  note "uxsm $version on $d"
  rc=0
  run "$d" || rc=$?
  case $rc in
  0) result=ok ;;
  75) result=MIRROR ;;
  *) result=FAILED ;;
  esac
  note "$d: $result"

  durations "$log" >"$logdir/times"
  {
    echo "== $d: $result, log in $logdir"
    sed 's/^/  /' "$logdir/times"
    if [ "$rc" -ne 0 ]; then
      echo "== last lines of the $d log:"
      tail -n 30 "$log" | sed 's/^/  /'
    fi
  } >"$logdir/block"
  cat "$logdir/block" >&2
  rm -f "$logdir/block"
  echo "$rc $result" >"$results/$d"

  # Keep the ten latest runs of this distribution.
  find "$logs/$d" -mindepth 1 -maxdepth 1 -type d | LC_ALL=C sort -r | tail -n +11 | xargs -r rm -rf
}

# Up to $jobs runs at a time. Each one takes a token from a FIFO before it
# starts and puts it back when it ends; the token is the SSH port of its VMs,
# so two machines never share one. After a failure no further run starts, and
# the ones under way finish: stopping them would only waste what they did.
results=$(mktemp -d)
mkfifo "$results/tokens"
exec 8<>"$results/tokens"
i=0
while [ "$i" -lt "$jobs" ]; do
  echo $((2222 + i)) >&8
  i=$((i + 1))
done
for d in $distros; do
  if [ -n "$publish" ] && since=$(has_passed "$d"); then
    echo "0 ok (key from $since)" >"$results/$d"
    continue
  fi
  read -r port <&8
  if [ -f "$results/stop" ]; then
    echo "$port" >&8
    continue
  fi
  (
    UXSM_VM_SSH_PORT=$port
    export UXSM_VM_SSH_PORT
    # Whatever happens to the run, the token goes back, or the loop would
    # wait for it forever.
    one "$d" || echo "1 FAILED" >"$results/$d"
    read -r rc _ <"$results/$d" || rc=1
    [ "$rc" -eq 0 ] || : >"$results/stop"
    echo "$port" >&8
  ) &
done
wait

summary=
status=0
for d in $distros; do
  if [ -f "$results/$d" ]; then
    read -r rc result <"$results/$d"
  else
    rc=0 result="not run"
  fi
  [ "$status" -ne 0 ] || status=$rc
  summary="$summary$d: $result\n"
done
rm -rf "$results"

printf '\n%b' "$summary" >&2

(cd "$out" && find . -type f ! -name version ! -name SHA256SUMS -printf '%P\n' | LC_ALL=C sort | xargs sha256sum >SHA256SUMS)

if [ -n "$publish" ] && [ "$status" -eq 0 ]; then
  rm -rf releases/latest.new
  mkdir -p releases
  cp -r "$out" releases/latest.new
  rm -rf releases/latest
  mv releases/latest.new releases/latest
  echo "== releases/latest is uxsm $version" >&2
fi
exit "$status"

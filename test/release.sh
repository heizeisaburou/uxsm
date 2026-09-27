#!/bin/sh
# Build uxsm packages and test them after installation, entirely in disposable
# virtual machines through the test/vm.sh runner:
#
#   1. Archive the working tree, including uncommitted changes and new files not
#      ignored by git.
#   2. Build each distribution's package in a VM of that distribution with its
#      packaging tool, then copy it under build/release/<distro>
#   3. On a clean VM for each distribution, install its package through its
#      package manager and run the test/integration/run.sh suite.
#   4. With --publish, after success, promote build/release to the
#      releases/latest directory.
#
#   test/release.sh [distros]
#   test/release.sh --publish
#
# With FAST=1, steps 2 and 3 share a VM, saving one boot and one dependency
# installation per distribution. This loses the clean-machine guarantee: a clean
# VM has only the package and test requirements, exposing undeclared runtime
# dependencies. FAST=1 is therefore for development, not publishing; --publish
# always uses separate VMs.
#
#   distros    quick  ubuntu (default)
#              pair   ubuntu and arch, the two most different
#              all    ubuntu, debian, arch, fedora, and opensuse
#              or a comma-separated list: ubuntu,arch
#
# Each distribution has its own package even when formats match: Debian and
# Ubuntu build against their own libraries and tools, as do Fedora and openSUSE.
# --publish builds and tests every distribution, so releases/latest is always a
# complete set.

set -eu
cd "$(dirname "$0")/.."

fast=${FAST:-}
publish=
if [ "${1:-}" = --publish ]; then
    publish=1
    shift
    [ $# -eq 0 ] || { echo "release.sh: --publish always tests every distro" >&2; exit 2; }
    [ -z "$fast" ] || { echo "release.sh: --publish always builds and tests on separate machines" >&2; exit 2; }
    set -- all
fi

case "${1:-quick}" in
quick) distros=ubuntu ;;
pair)  distros="ubuntu arch" ;;
all)   distros="ubuntu debian arch fedora opensuse" ;;
*)     distros=$(printf '%s' "$1" | tr ',' ' ') ;;
esac

# recipe DISTRO: the test/package script that builds its package.
recipe() {
    case "$1" in
    ubuntu | debian)   echo deb ;;
    arch)              echo arch ;;
    fedora | opensuse) echo rpm ;;
    *)                 return 1 ;;
    esac
}

for d in $distros; do
    recipe "$d" >/dev/null || { echo "release.sh: no package for $d yet" >&2; exit 2; }
done

# Package version comes from the test/version.sh script.
version=$(test/version.sh)

out=build/release
rm -rf "$out" build/vm
mkdir -p "$out" build/vm
echo "$version" >"$out/version"

if [ -n "$fast" ]; then
    echo "== uxsm $version: building and testing on $distros, one machine each" >&2
else
    echo "== uxsm $version: building and testing on $distros" >&2
fi

# Tarball: files known to git or not ignored by it, excluding deleted files.
tarball=uxsm-$version.tar.gz
git ls-files --cached --others --exclude-standard | while IFS= read -r f; do
    if [ -e "$f" ]; then printf '%s\n' "$f"; fi
done | LC_ALL=C sort |
    tar --create --file=- --files-from=- --transform="s|^|uxsm-$version/|" \
        --owner=0 --group=0 --numeric-owner |
    gzip -n >"$out/$tarball"

for d in $distros; do
    stage=build/vm/build-$d
    mkdir -p "$stage"
    cp "$out/version" "$out/$tarball" "$stage/"
    cp "test/package/$(recipe "$d").sh" "$stage/package.sh"
    # With FAST, the same VM builds the package and runs tests with it installed:
    # package.sh leaves it in ~/uxsm/out, where run.sh finds it and vm.sh
    # downloads it afterwards.
    if [ -n "$fast" ]; then
        cp -r test/integration "$stage/integration"
        UXSM_VM_UPLOAD=$stage UXSM_VM_DOWNLOAD=$out/$d \
            test/vm.sh "$d" "sh ~/uxsm/package.sh &&
                mkdir -p ~/uxsm/packages &&
                cp -r ~/uxsm/out ~/uxsm/packages/$d &&
                sh ~/uxsm/integration/run.sh"
    else
        UXSM_VM_UPLOAD=$stage UXSM_VM_DOWNLOAD=$out/$d \
            test/vm.sh "$d" 'sh ~/uxsm/package.sh'
    fi
done

# Without FAST, a clean VM for each distribution installs the contents of
# packages/<distro> and runs the tests.
if [ -z "$fast" ]; then
    stage=build/vm/test
    mkdir -p "$stage/packages"
    cp -r test/integration "$stage/integration"
    cp "$out/version" "$stage/"
    for d in $distros; do
        cp -r "$out/$d" "$stage/packages/$d"
    done
    UXSM_VM_UPLOAD=$stage test/vm.sh "$(echo $distros | tr ' ' ',')" 'sh ~/uxsm/integration/run.sh'
fi

(cd "$out" && find . -type f ! -name version ! -name SHA256SUMS -printf '%P\n' | LC_ALL=C sort | xargs sha256sum >SHA256SUMS)

if [ -n "$publish" ]; then
    rm -rf releases/latest.new
    mkdir -p releases
    cp -r "$out" releases/latest.new
    rm -rf releases/latest
    mv releases/latest.new releases/latest
    echo "== releases/latest is uxsm $version" >&2
fi

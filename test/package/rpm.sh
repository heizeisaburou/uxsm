#!/bin/sh
# Build the VM distribution's .rpm package, Fedora or openSUSE, using its .spec
# from packaging/ and rpmbuild. test/release.sh runs this after uploading
# ~/uxsm/version and ~/uxsm/uxsm-<version>.tar.gz; packages are left in
# the ~/uxsm/out directory.
#
# The .spec downloads the GitHub tag tarball. rpmbuild looks in SOURCES for the
# final component of Source0, so setting the version and placing the tarball
# there is sufficient.

set -eu
cd "$HOME/uxsm"
v=$(cat version)

if command -v dnf >/dev/null 2>&1; then
    pm_install() { sudo dnf -y -q install "$@" >/dev/null; }
else
    pm_install() {
        sudo zypper --non-interactive --quiet install --no-recommends "$@" >/dev/null
    }
    sudo zypper --non-interactive --quiet refresh >/dev/null
fi

mkdir -p "$HOME/rpmbuild/SOURCES" "$HOME/rpmbuild/SPECS"
cp "uxsm-$v.tar.gz" "$HOME/rpmbuild/SOURCES/"
spec=$HOME/rpmbuild/SPECS/uxsm.spec
tar -xzOf "uxsm-$v.tar.gz" "uxsm-$v/packaging/$UXSM_VM_DISTRO/uxsm.spec" >"$spec"
sed -i "s/^Version:\( *\).*/Version:\1$v/" "$spec"

echo "== installing build dependencies"
pm_install rpm-build
# Install BuildRequires from the .spec, so a missing dependency fails here.
buildrequires=$(rpmspec -q --buildrequires "$spec")
set --
while IFS= read -r dep; do
    set -- "$@" "$dep"
done <<EOF
$buildrequires
EOF
pm_install "$@"

echo "== building uxsm $v"
rpmbuild -bb "$spec"

mkdir -p "$HOME/uxsm/out"
mv "$HOME"/rpmbuild/RPMS/*/*.rpm "$HOME/uxsm/out/"
ls "$HOME/uxsm/out"

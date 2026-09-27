#!/bin/sh
# Build the VM distribution's .deb package, Debian or Ubuntu, using
# packaging/debian and dpkg-buildpackage. test/release.sh runs this after
# uploading ~/uxsm/version and ~/uxsm/uxsm-<version>.tar.gz; the package is left
# in the ~/uxsm/out directory.

set -eu
cd "$HOME/uxsm"
v=$(cat version)
. /etc/os-release

apt() {
    sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 apt-get -y -qq "$@" >/dev/null
}

mkdir build
cd build
cp "../uxsm-$v.tar.gz" "uxsm_$v.orig.tar.gz"
tar -xzf "uxsm_$v.orig.tar.gz"
cd "uxsm-$v"
cp -r packaging/debian debian
# The changelog's first line supplies the package version. The revision includes
# the distribution, -1~ubuntu24.04 or -1~debian13, giving each package a distinct
# name; ~ also makes an official package's -1 sort later.
sed -i "1s/([^)]*)/($v-1~$ID$VERSION_ID)/" debian/changelog

echo "== installing build dependencies"
apt update
apt install --no-install-recommends build-essential dpkg-dev
# Install Build-Depends from debian/control, so a missing dependency fails here.
apt build-dep ./

echo "== building uxsm $v"
dpkg-buildpackage -us -uc -b

mkdir -p "$HOME/uxsm/out"
mv ../*.deb "$HOME/uxsm/out/"
ls "$HOME/uxsm/out"

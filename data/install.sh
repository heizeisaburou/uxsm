#!/bin/sh
# Installer shipped inside the binary tarball, for machines where uxsm is not
# packaged: it puts the same files in the same places as `make install`, without
# needing Go or a package manager.
#
# It lives in the repository so it is reviewed and linted with everything else;
# `make bindist` copies it into the tarball.

set -eu
cd "$(dirname "$0")"

prefix=/usr/local
destdir=
uninstall=

usage() {
    cat <<USAGE
Usage: ./install.sh [--prefix DIR] [--destdir DIR] [--uninstall]

Install uxsm: the binary, the systemd user units and the manual page.

  --prefix DIR    where to install, /usr/local by default
  --destdir DIR   a root to install under, for packaging
  --uninstall     remove what this script installs

systemd reads user units from both /usr/lib/systemd/user and
/usr/local/lib/systemd/user, so either prefix works.
USAGE
}

while [ $# -gt 0 ]; do
    case $1 in
    --prefix) prefix=${2:?--prefix needs a directory}; shift 2 ;;
    --destdir) destdir=${2:?--destdir needs a directory}; shift 2 ;;
    --uninstall) uninstall=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
    esac
done

bindir=$prefix/bin
unitdir=$prefix/lib/systemd/user
mandir=$prefix/share/man/man1
version=$(cat version)

# The units and the manual page are templates: @BINDIR@ is filled in here, so
# the same tarball installs under any prefix.
units=$(ls systemd/user/*.in 2>/dev/null || true)
pages=$(ls man/*.1.in 2>/dev/null || true)

if [ -n "$uninstall" ]; then
    rm -f "$destdir$bindir/uxsm"
    for f in $units; do rm -f "$destdir$unitdir/$(basename "$f" .in)"; done
    for f in $pages; do rm -f "$destdir$mandir/$(basename "$f" .in)"; done
    echo "Removed uxsm from $destdir$prefix"
    exit 0
fi

# A clear message beats a permission error halfway through.
if ! mkdir -p "$destdir$bindir" 2>/dev/null; then
    echo "install.sh: cannot write to $destdir$bindir; run it as root or pass --prefix" >&2
    exit 1
fi

install -Dm755 uxsm "$destdir$bindir/uxsm"
for f in $units; do
    unit="$destdir$unitdir/$(basename "$f" .in)"
    mkdir -p "$destdir$unitdir"
    sed "s|@BINDIR@|$bindir|g" "$f" >"$unit"
    chmod 644 "$unit"
done
for f in $pages; do
    page="$destdir$mandir/$(basename "$f" .in)"
    mkdir -p "$destdir$mandir"
    sed "s|@BINDIR@|$bindir|g;s|@VERSION@|$version|g" "$f" >"$page"
    chmod 644 "$page"
done

# No full stop after a path: it can be copied from the terminal with a double
# click.
cat <<DONE
Installed uxsm $version under
  $destdir$prefix

Next: make the display manager offer a uxsm session
  uxsm check
  uxsm entry <your window manager>
DONE

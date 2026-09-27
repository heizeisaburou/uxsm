#!/bin/sh
# Collect X11 session entries shipped by packages for the VM's distribution
# without installing them: search file indexes for packages containing files in
# /usr/share/xsessions, download them without dependencies, and extract only
# those entries. test/xsessions.sh runs this inside a test/vm.sh machine.
#
# Leave entries unchanged in ~/uxsm/out and one "file package version" line per
# entry in the ~/uxsm/out/index file.

set -eu
out=$HOME/uxsm/out
mkdir -p "$out" "$HOME/dl"
cd "$HOME/dl"

if command -v pacman >/dev/null 2>&1; then
    sudo pacman -Sy --noconfirm >/dev/null
    sudo pacman -Fy --noconfirm >/dev/null
    # "usr/share/xsessions/i3.desktop is owned by extra/i3-wm 4.25.1-1"
    pacman -Fx '^usr/share/xsessions/.+\.desktop$' |
        sed -E 's|^usr/share/xsessions/(\S+) is owned by [^/]+/(\S+) (\S+)$|\1 \2 \3|' >"$out/index"
    # -dd: no dependencies; packages remain in pacman's cache.
    cut -d' ' -f2 "$out/index" | sort -u | xargs sudo pacman -Sddw --noconfirm >/dev/null
    for p in /var/cache/pacman/pkg/*.pkg.tar.zst; do
        bsdtar -xf "$p" -C "$HOME/dl" 'usr/share/xsessions/*' 2>/dev/null || true
    done

elif command -v apt-get >/dev/null 2>&1; then
    apt() { sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 apt-get -y -qq "$@" >/dev/null; }
    apt update
    apt install apt-file
    sudo apt-file update >/dev/null
    # "i3-wm: /usr/share/xsessions/i3.desktop"
    apt-file search -x '^/usr/share/xsessions/.+\.desktop$' |
        sed -E 's|^(\S+): /usr/share/xsessions/(\S+)$|\2 \1|' |
        while read -r f p; do
            echo "$f $p $(apt-cache policy "$p" | sed -n 's/^ *Candidate: //p')"
        done >"$out/index"
    cut -d' ' -f2 "$out/index" | sort -u | xargs apt-get download -qq >/dev/null
    for p in ./*.deb; do
        dpkg-deb --fsys-tarfile "$p" | tar -x --wildcards './usr/share/xsessions/*' 2>/dev/null || true
    done

elif command -v dnf >/dev/null 2>&1; then
    # dnf does not load file lists unless requested. Build the index from
    # downloaded packages because one dnf query per package is very slow.
    dnf -q repoquery --latest-limit 1 --setopt=optional_metadata_types=filelists \
        --qf '%{name}\n' --file '/usr/share/xsessions/*' | sort -u | xargs dnf -q download >/dev/null
    for p in ./*.rpm; do
        nv=$(rpm -qp --qf '%{NAME} %{VERSION}-%{RELEASE}' "$p" 2>/dev/null)
        for f in $(rpm -qlp "$p" 2>/dev/null | sed -n 's|^/usr/share/xsessions/\(.*\.desktop\)$|\1|p'); do
            echo "$f $nv"
        done
        rpm2archive - <"$p" | tar -xz --wildcards './usr/share/xsessions/*' 2>/dev/null || true
    done >"$out/index"

else
    echo "collect.sh: no supported package manager" >&2
    exit 1
fi

cp -P "$HOME"/dl/usr/share/xsessions/*.desktop "$out/"
echo "$(wc -l <"$out/index") entries in $(cut -d' ' -f2 "$out/index" | sort -u | wc -l) packages"

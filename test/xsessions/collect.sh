#!/bin/sh
# Recoge las entradas de sesión X11 que trae cada paquete de la distribución de
# la máquina, sin instalar ninguno: busca en los índices de ficheros qué
# paquetes tienen algo en /usr/share/xsessions, los descarga sin dependencias y
# extrae sólo esas entradas. Lo ejecuta test/xsessions.sh dentro de una máquina
# de test/vm.sh.
#
# Deja en ~/uxsm/out las entradas tal cual, y en ~/uxsm/out/index una línea por
# entrada: «fichero paquete versión».

set -eu
out=$HOME/uxsm/out
mkdir -p "$out" "$HOME/dl"
cd "$HOME/dl"

if command -v pacman >/dev/null 2>&1; then
    sudo pacman -Sy --noconfirm >/dev/null
    sudo pacman -Fy --noconfirm >/dev/null
    # «usr/share/xsessions/i3.desktop is owned by extra/i3-wm 4.25.1-1»
    pacman -Fx '^usr/share/xsessions/.+\.desktop$' |
        sed -E 's|^usr/share/xsessions/(\S+) is owned by [^/]+/(\S+) (\S+)$|\1 \2 \3|' >"$out/index"
    # -dd: sin dependencias; los paquetes se quedan en la caché de pacman.
    cut -d' ' -f2 "$out/index" | sort -u | xargs sudo pacman -Sddw --noconfirm >/dev/null
    for p in /var/cache/pacman/pkg/*.pkg.tar.zst; do
        bsdtar -xf "$p" -C "$HOME/dl" 'usr/share/xsessions/*' 2>/dev/null || true
    done

elif command -v apt-get >/dev/null 2>&1; then
    apt() { sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 apt-get -y -qq "$@" >/dev/null; }
    apt update
    apt install apt-file
    sudo apt-file update >/dev/null
    # «i3-wm: /usr/share/xsessions/i3.desktop»
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
    # dnf no carga las listas de ficheros si no se le pide. El índice sale de
    # los paquetes ya descargados: una consulta por paquete a dnf es muy lenta.
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

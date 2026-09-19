#!/bin/sh
# Escribe la versión de los paquetes que se compilarían con el árbol de trabajo
# actual. Vale tanto como pkgver de Arch, que no admite guiones, como versión
# upstream de Debian:
#
#   X.Y.Z                 justo en la etiqueta vX.Y.Z (0.0.0 si no hay ninguna)
#   X.Y.Z.rN.gHASH        N commits después de ella, HASH el último
#   ….dirty               con cambios sin commitear o ficheros nuevos

set -eu
cd "$(dirname "$0")/.."

if tag=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null); then
    version=${tag#v}
    commits=$(git rev-list --count "$tag..HEAD")
else
    version=0.0.0
    commits=$(git rev-list --count HEAD)
fi
if [ "$commits" -gt 0 ]; then
    version=$version.r$commits.g$(git rev-parse --short HEAD)
fi
if [ -n "$(git status --porcelain)" ]; then
    version=$version.dirty
fi
echo "$version"

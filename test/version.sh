#!/bin/sh
# Print the package version that would be built from the current working tree.
# It is valid both as an Arch pkgver, which forbids dashes, and as a Debian
# upstream version:
#
#   X.Y.Z                 exactly at tag vX.Y.Z (0.0.0 if no tag exists)
#   X.Y.Z.rN.gHASH        N commits after it, with HASH from the latest commit
#   ….dirty               with uncommitted changes or new files

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

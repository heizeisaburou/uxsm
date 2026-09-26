#!/bin/sh
# Publish a version: the tag, the push, and the GitHub release.
#
#   test/publish.sh [--dry-run] [--yes] vX.Y.Z
#   test/publish.sh --hashes vX.Y.Z
#
# The release build is what `make release` already does; this only publishes it,
# in this order, stopping at the first thing that does not add up:
#
#   1. Checks: the version, a clean tree, the branch, the tag nowhere yet, no
#      release with that name, and gh logged in.
#   2. The tag, created locally. From then on the version is X.Y.Z, so the
#      packages and both tarballs have to be built with the tag in place: if
#      releases/latest is not that version, `make release` runs now.
#   3. The push of the commit and the tag, and the GitHub release with the five
#      packages, the source tarball, the binary tarball and SHA256SUMS.
#
# Steps 1 and 2 change nothing outside this clone; the tag is undone with
# `git tag -d`. Step 3 is public and cannot be undone, so it asks first unless
# --yes. With --dry-run nothing runs: it says what it would do.
#
# --hashes goes afterwards, once GitHub serves the tag: it prints the checksum of
# that tarball and writes it into the Arch and Nix recipes, which is what the AUR
# and nixpkgs need. It cannot be done before the tag: GitHub compresses its
# tarball its own way, so the checksum is only known once it exists.

set -eu
cd "$(dirname "$0")/.."

dry=
yes=
hashes=
while [ $# -gt 1 ]; do
    case $1 in
    --dry-run) dry=1 ;;
    --yes) yes=1 ;;
    --hashes) hashes=1 ;;
    *) echo "publish.sh: unknown option $1" >&2; exit 2 ;;
    esac
    shift
done
tag=${1:-}

die() { echo "publish.sh: $*" >&2; exit 1; }
run() {
    if [ -n "$dry" ]; then
        echo "would run: $*"
        return 0
    fi
    "$@"
}

case $tag in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) die "the version goes as vX.Y.Z, not ${tag:-nothing}" ;;
esac
version=${tag#v}
repo=heizeisaburou/uxsm

command -v gh >/dev/null 2>&1 || die "gh is not installed, and the release is published with it"
gh auth status >/dev/null 2>&1 || die "gh is not logged in: run gh auth login"

if [ -n "$hashes" ]; then
    url="https://github.com/$repo/archive/refs/tags/$tag.tar.gz"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    echo "== downloading $url"
    curl -fsSL "$url" -o "$tmp/src.tar.gz" || die "GitHub does not serve $tag yet"
    sum=$(sha256sum "$tmp/src.tar.gz" | cut -d' ' -f1)
    echo "sha256 of the tag tarball: $sum"
    run sed -i "s/^pkgver=.*/pkgver=$version/;s/^sha256sums=.*/sha256sums=('$sum')/" packaging/arch/PKGBUILD
    if command -v nix >/dev/null 2>&1; then
        sri=$(nix hash convert --hash-algo sha256 "$sum" 2>/dev/null ||
            nix hash to-sri --type sha256 "$sum" 2>/dev/null || echo)
    fi
    if [ -n "${sri:-}" ]; then
        run sed -i "s|hash = lib.fakeHash;|hash = \"$sri\";|;s|hash = \".*\";|hash = \"$sri\";|;s/^  version = \".*\";/  version = \"$version\";/" packaging/nix/package.nix
    else
        echo "nix is not here: put sha256-<base64> in packaging/nix/package.nix by hand" >&2
    fi
    echo "Recipes updated; review and commit them. They are what the AUR and nixpkgs read."
    exit 0
fi

# 1. Checks.
[ -z "$(git status --porcelain)" ] || die "the working tree has changes; commit or stash them first"
branch=$(git rev-parse --abbrev-ref HEAD)
case $branch in
main | master) ;;
*) die "publishing is done from main or master, not from $branch" ;;
esac
! git rev-parse -q --verify "refs/tags/$tag" >/dev/null || die "the tag $tag already exists here; delete it with git tag -d $tag if it is wrong"
[ -z "$(git ls-remote --tags origin "refs/tags/$tag")" ] || die "the tag $tag is already pushed, so that version is published"
! gh release view "$tag" --repo "$repo" >/dev/null 2>&1 || die "there is already a GitHub release called $tag"
[ -z "$(git log "@{upstream}..HEAD" --oneline 2>/dev/null || true)" ] ||
    echo "publish.sh: note: this commit is not pushed yet; it goes up with the tag" >&2

echo "== uxsm $version from $(git rev-parse --short HEAD) on $branch"

# 2. The tag, and the release build with the tag in place: test/version.sh reads
# the tag, so the packages only carry X.Y.Z once it exists.
run git tag -a "$tag" -m "uxsm $version"
if [ -n "$dry" ]; then
    echo "would run: make release, unless releases/latest is already uxsm $version"
else
    if [ "$(cat releases/latest/version 2>/dev/null)" = "$version" ]; then
        echo "== releases/latest is uxsm $version, already built and tested"
    else
        echo "== releases/latest is not uxsm $version: building and testing every distribution"
        make release || die "make release failed; the tag is still only here: git tag -d $tag"
    fi
fi

# The binary tarball goes next to the packages, and into the checksums with them.
run make bindist VERSION="$version" DISTDIR=releases/latest
if [ -z "$dry" ]; then
    (cd releases/latest &&
        find . -type f ! -name version ! -name SHA256SUMS -printf '%P\n' |
        LC_ALL=C sort | xargs sha256sum >SHA256SUMS)
    # What is about to be published, installed from the tarball into a temporary
    # root: if the tarball does not install, nobody should be downloading it.
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    tar xzf "releases/latest/uxsm-$version-linux-$(uname -m).tar.gz" -C "$tmp"
    (cd "$tmp"/uxsm-* && ./install.sh --destdir "$tmp/root" --prefix /usr >/dev/null)
    [ -x "$tmp/root/usr/bin/uxsm" ] || die "the binary tarball does not install its binary"
    [ -f "$tmp/root/usr/share/man/man1/uxsm.1" ] || die "the binary tarball does not install the manual page"
    got=$("$tmp/root/usr/bin/uxsm" version)
    [ "$got" = "$version" ] || die "the binary in the tarball says it is $got, not $version"
    echo "== the binary tarball installs and reports uxsm $version"
fi

if [ ! -d releases/latest ]; then
    [ -n "$dry" ] || die "releases/latest is not there after make release"
    echo "== would publish what make release leaves in releases/latest"
    artifacts=
else
    # Every distribution has to have left its package: a release with four is not
    # a release.
    for d in ubuntu debian arch fedora opensuse; do
        [ -n "$(find "releases/latest/$d" -type f 2>/dev/null)" ] ||
            die "releases/latest has no package for $d"
    done
    artifacts=$(find releases/latest -type f ! -name version | LC_ALL=C sort)
    echo "== would publish:"
    echo "$artifacts" | sed 's|^|  |'
fi

# 3. Public, and it cannot be undone.
if [ -z "$dry" ] && [ -z "$yes" ]; then
    printf 'Push %s and publish the release of uxsm %s? [y/N] ' "$tag" "$version"
    read -r answer
    case $answer in
    y | Y | yes) ;;
    *) die "nothing pushed; the tag is still only here: git tag -d $tag" ;;
    esac
fi

run git push origin "$branch"
run git push origin "$tag"
# shellcheck disable=SC2086
run gh release create "$tag" --repo "$repo" --title "uxsm $version" \
    --notes "Packages for Ubuntu, Debian, Arch, Fedora and openSUSE, the source tarball, and a binary tarball with its own install.sh for machines with no uxsm package. Checksums in SHA256SUMS." \
    $artifacts

echo "== published: https://github.com/$repo/releases/tag/$tag"
echo "Next, once GitHub serves the tarball: test/publish.sh --hashes $tag"

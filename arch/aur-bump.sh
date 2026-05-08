#!/usr/bin/env bash
# Bump the pikvm-bin AUR package to the version of the latest GitHub release.
#
# Usage:  ./arch/aur-bump.sh <version>     # e.g. 0.1.0  (no leading 'v')
#     or  make aur-bump VER=0.1.0
#
# Requires:
#   - SSH access to aur.archlinux.org as your AUR user
#   - The matching release already published at
#       https://github.com/j4y-w4lk3r/pikvm/releases/tag/v<version>
#
# What it does:
#   1. Clones (or updates) the AUR pikvm-bin repo into /tmp/
#   2. Updates pkgver, pkgrel, and the source SHA256s by curl'ing the
#      uploaded release tarballs
#   3. Regenerates .SRCINFO via makepkg --printsrcinfo
#   4. Commits + pushes back to the AUR

set -euo pipefail

VER="${1:-${VER:-}}"
if [[ -z "$VER" ]]; then
    echo "usage: $0 <version>     # e.g. 0.1.0 (no leading 'v')" >&2
    exit 1
fi

REPO="https://github.com/j4y-w4lk3r/pikvm"
AUR="ssh://aur@aur.archlinux.org/pikvm-bin.git"
WORK="/tmp/pikvm-aur"

echo ">>> Cloning AUR repo into $WORK"
rm -rf "$WORK"
git clone "$AUR" "$WORK"

cd "$WORK"

echo ">>> Fetching release SHA256s for v${VER}"
url_x86="${REPO}/releases/download/v${VER}/pikvm_${VER}_linux_x86_64.tar.gz"
url_arm="${REPO}/releases/download/v${VER}/pikvm_${VER}_linux_arm64.tar.gz"

sha_x86=$(curl -fsSL "$url_x86" | sha256sum | awk '{print $1}')
sha_arm=$(curl -fsSL "$url_arm" | sha256sum | awk '{print $1}')
echo "    x86_64:  $sha_x86"
echo "    aarch64: $sha_arm"

echo ">>> Patching PKGBUILD"
# pkgver= line — replace right-hand side
sed -i.bak "s|^pkgver=.*|pkgver=${VER}|" PKGBUILD
sed -i.bak "s|^pkgrel=.*|pkgrel=1|" PKGBUILD

# sha256sums_x86_64 / aarch64 — first matching line (inside parens). Simple
# single-element arrays make this safe.
sed -i.bak "s|^sha256sums_x86_64=.*|sha256sums_x86_64=('${sha_x86}')|" PKGBUILD
sed -i.bak "s|^sha256sums_aarch64=.*|sha256sums_aarch64=('${sha_arm}')|" PKGBUILD
rm -f PKGBUILD.bak

echo ">>> Regenerating .SRCINFO"
if command -v makepkg >/dev/null 2>&1; then
    makepkg --printsrcinfo > .SRCINFO
else
    echo "    (no makepkg available — copying PKGBUILD-based SRCINFO from repo source)" >&2
    # Fallback: a hand-written .SRCINFO update. Kept simple — sufficient for AUR.
    sed -i.bak "s|pkgver = .*|pkgver = ${VER}|" .SRCINFO
    sed -i.bak "s|sha256sums_x86_64 = .*|sha256sums_x86_64 = ${sha_x86}|" .SRCINFO
    sed -i.bak "s|sha256sums_aarch64 = .*|sha256sums_aarch64 = ${sha_arm}|" .SRCINFO
    sed -i.bak "s|/v[0-9.]*/pikvm_[0-9.]*_linux_x86_64|/v${VER}/pikvm_${VER}_linux_x86_64|" .SRCINFO
    sed -i.bak "s|/v[0-9.]*/pikvm_[0-9.]*_linux_arm64|/v${VER}/pikvm_${VER}_linux_arm64|" .SRCINFO
    sed -i.bak "s|pikvm-bin-[0-9.]*-x86_64|pikvm-bin-${VER}-x86_64|" .SRCINFO
    sed -i.bak "s|pikvm-bin-[0-9.]*-aarch64|pikvm-bin-${VER}-aarch64|" .SRCINFO
    rm -f .SRCINFO.bak
fi

echo ">>> Committing + pushing"
git add PKGBUILD .SRCINFO
git commit -m "pikvm-bin: bump to ${VER}"
git push origin HEAD:master

echo
echo "✓ pikvm-bin bumped to v${VER} on AUR."
echo "  https://aur.archlinux.org/packages/pikvm-bin"

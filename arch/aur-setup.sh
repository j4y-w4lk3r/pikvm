#!/usr/bin/env bash
# One-time setup helper for the pikvm-bin AUR package.
#
# What this automates (everything except the AUR website registration):
#   1. Generates a dedicated SSH key at ~/.ssh/aur_ed25519
#   2. (Optional) saves the key passphrase into 1Password via `op`
#   3. Idempotently appends the Host-block to ~/.ssh/config
#   4. Copies the public key to the clipboard (macOS pbcopy)
#   5. Opens the AUR "My Account" page in your browser
#   6. Waits for you to paste + save it on the AUR site
#   7. Smoke-tests `ssh aur@aur.archlinux.org help`
#   8. Bootstraps the pikvm-bin AUR repo with PKGBUILD + .SRCINFO from this repo
#
# Run from the repo root:
#   bash arch/aur-setup.sh
#
# Prerequisites:
#   - You've already registered an AUR account at https://aur.archlinux.org/register
#   - macOS or Linux with: ssh-keygen, ssh, git, pbcopy/xclip (optional), op (optional)

set -euo pipefail

KEY="$HOME/.ssh/aur_ed25519"
CFG="$HOME/.ssh/config"
HOST_BLOCK="Host aur.archlinux.org"
AUR_HOST="aur@aur.archlinux.org"
PIKVM_REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK="/tmp/pikvm-aur-bootstrap"

green() { printf '\033[0;32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[1;33m%s\033[0m\n' "$*"; }
red() { printf '\033[0;31m%s\033[0m\n' "$*" >&2; }
ask() { read -r -p "$1 [y/N]: " ans; [[ "$ans" =~ ^[Yy] ]]; }

# ---------------------------------------------------------------------------
# 1. SSH key
# ---------------------------------------------------------------------------

if [ -f "$KEY" ]; then
    yellow "↻  $KEY already exists — keeping it (delete first if you want to regenerate)"
else
    green "→  Generating dedicated AUR SSH key at $KEY"
    PASSPHRASE=""
    if ask "    Use a passphrase? (more secure; can be saved to 1Password below)"; then
        read -r -s -p "    Passphrase: " PASSPHRASE; echo
    fi
    ssh-keygen -t ed25519 -f "$KEY" -C "aur@$USER" -N "$PASSPHRASE"
    green "✓  Key generated"

    # Optional: save passphrase to 1Password
    if [ -n "$PASSPHRASE" ] && command -v op >/dev/null 2>&1; then
        if ask "    Save this passphrase to 1Password under 'AUR SSH Key'?"; then
            op item create \
                --category "Secure Note" \
                --title "AUR SSH Key" \
                --vault "Private" \
                "passphrase=$PASSPHRASE" \
                "username=$(whoami)" \
                "key_path=$KEY" \
                "notes=Passphrase for the dedicated AUR ed25519 SSH key. Used for ssh aur@aur.archlinux.org and pushing AUR packages." \
                >/dev/null
            green "✓  Saved to 1Password — retrieve with: op read 'op://Private/AUR SSH Key/passphrase'"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# 2. ~/.ssh/config
# ---------------------------------------------------------------------------

if [ -f "$CFG" ] && grep -qF "$HOST_BLOCK" "$CFG"; then
    yellow "↻  ~/.ssh/config already has a Host aur.archlinux.org block — leaving it alone"
else
    green "→  Appending Host block to ~/.ssh/config"
    mkdir -p "$(dirname "$CFG")"
    {
        echo
        echo "# AUR (Arch User Repository) — added by arch/aur-setup.sh"
        echo "$HOST_BLOCK"
        echo "    IdentityFile $KEY"
        echo "    User aur"
    } >> "$CFG"
    green "✓  Added"
fi

# ---------------------------------------------------------------------------
# 3. Copy public key to clipboard + open AUR "My Account"
# ---------------------------------------------------------------------------

PUB_KEY=$(cat "${KEY}.pub")
green "→  Public key contents:"
echo "    $PUB_KEY"

if command -v pbcopy >/dev/null 2>&1; then
    printf '%s' "$PUB_KEY" | pbcopy
    green "✓  Copied to macOS clipboard"
elif command -v xclip >/dev/null 2>&1; then
    printf '%s' "$PUB_KEY" | xclip -selection clipboard
    green "✓  Copied to X11 clipboard"
elif command -v wl-copy >/dev/null 2>&1; then
    printf '%s' "$PUB_KEY" | wl-copy
    green "✓  Copied to Wayland clipboard"
fi

if command -v open >/dev/null 2>&1; then
    open "https://aur.archlinux.org/account/" >/dev/null 2>&1 || true
elif command -v xdg-open >/dev/null 2>&1; then
    xdg-open "https://aur.archlinux.org/account/" >/dev/null 2>&1 || true
fi

cat <<EOF

Now in your browser:
  1. Log in to AUR if needed
  2. Click your username (top-right) → "My Account"
  3. Scroll to "SSH Public Key", paste the key (already on your clipboard)
  4. Click "Update"

EOF
read -r -p "Press Enter once the SSH key is saved on the AUR account: " _

# ---------------------------------------------------------------------------
# 4. Smoke test
# ---------------------------------------------------------------------------

green "→  Smoke-testing ssh $AUR_HOST help"
if ssh -o StrictHostKeyChecking=accept-new "$AUR_HOST" help 2>&1 | head -3; then
    green "✓  SSH to AUR works"
else
    red "✗  SSH to AUR failed. Check that you pasted the FULL key content into the AUR site."
    exit 1
fi

# ---------------------------------------------------------------------------
# 5. Bootstrap pikvm-bin
# ---------------------------------------------------------------------------

if ! ask "Bootstrap the pikvm-bin AUR repo now? (clones, copies PKGBUILD/.SRCINFO, pushes)"; then
    green "✓  Done. Run this script again later, or do the clone/push manually."
    exit 0
fi

rm -rf "$WORK"
git clone "ssh://${AUR_HOST}/pikvm-bin.git" "$WORK"

if [ -f "$WORK/PKGBUILD" ]; then
    yellow "↻  $WORK already has a PKGBUILD — pikvm-bin is already published on AUR."
    yellow "    Use \`make aur-bump VER=<version>\` to update for new releases."
    exit 0
fi

cp "$PIKVM_REPO_DIR/arch/PKGBUILD"  "$WORK/PKGBUILD"
cp "$PIKVM_REPO_DIR/arch/.SRCINFO"  "$WORK/.SRCINFO"

cd "$WORK"
git add PKGBUILD .SRCINFO
git -c user.name="$(git config --global --get user.name)" \
    -c user.email="$(git config --global --get user.email)" \
    commit -m "Initial import: pikvm-bin"
git push origin master

cat <<EOF

$(green "✓  pikvm-bin is now on AUR!")
   https://aur.archlinux.org/packages/pikvm-bin

After every GitHub release of j4y-w4lk3r/pikvm, bump it with:
   make aur-bump VER=<version>     # e.g. 0.1.0 (no leading 'v')

EOF

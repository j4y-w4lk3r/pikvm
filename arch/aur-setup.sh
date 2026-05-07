#!/usr/bin/env bash
# One-time setup helper for the pikvm-bin AUR package.
#
# What this automates (everything except the AUR website registration):
#   1. Generates a dedicated SSH key with one of three protection modes:
#        - YubiKey (FIDO2 ed25519-sk)  → ~/.ssh/aur_ed25519_sk
#        - Passphrase (with optional 1Password storage)  → ~/.ssh/aur_ed25519
#        - None (no passphrase)  → ~/.ssh/aur_ed25519
#   2. Idempotently appends the Host block to ~/.ssh/config
#   3. Copies the public key to the clipboard (macOS pbcopy / xclip / wl-copy)
#   4. Opens the AUR "My Account" page in your browser
#   5. Waits for you to paste + save it on the AUR site
#   6. Smoke-tests `ssh aur@aur.archlinux.org help`
#   7. Bootstraps the pikvm-bin AUR repo with PKGBUILD + .SRCINFO from this repo
#
# Run from the repo root:
#   bash arch/aur-setup.sh        # or:  make aur-setup
#
# Prerequisites:
#   - You've already registered an AUR account at https://aur.archlinux.org/register
#   - macOS or Linux with: ssh-keygen, ssh, git
#   - For YubiKey: OpenSSH ≥ 8.2 (macOS Sonoma+ has 9.0+) and a YubiKey 5+ with FIDO2
#   - Optional: pbcopy/xclip/wl-copy (clipboard), op (1Password CLI)

set -euo pipefail

KEY=""  # populated by generate_key based on the chosen protection mode
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
# 1. SSH key — choose hardware (YubiKey FIDO2) or software (passphrase / none)
# ---------------------------------------------------------------------------

generate_key_yubikey() {
    KEY="$HOME/.ssh/aur_ed25519_sk"
    if [ -f "$KEY" ] || [ -f "${KEY}.pub" ]; then
        yellow "↻  $KEY already exists — keeping it"
        return
    fi
    green "→  Generating YubiKey-backed (ed25519-sk) AUR SSH key"
    cat <<'EOF'
   You'll see one or two prompts from your YubiKey:
     - Touch the key when it blinks (always required)
     - Enter the FIDO2 PIN (if the key has one set)
EOF

    # Attempt 1: resident key on YubiKey (re-importable via `ssh-keygen -K`).
    # This requires a recent libfido2 ↔ YubiKey firmware combo and is still
    # flaky in 2026 — it commonly fails with "Key enrollment failed: invalid
    # format" on otherwise-working setups. We try it because the UX win is
    # nice when it works, but transparently fall back when it doesn't.
    rm -f "$KEY" "${KEY}.pub" 2>/dev/null
    if ssh-keygen -t ed25519-sk -f "$KEY" \
            -O resident -O application=ssh:aur \
            -C "aur-yubikey@$USER" 2>/tmp/ssh-keygen-err; then
        green "✓  Resident key created on YubiKey."
        green "   Re-import on another Mac later with:  ssh-keygen -K -f ~/.ssh/imported_aur"
        return
    fi

    # Attempt 2: non-resident ed25519-sk (the simpler form). The on-disk
    # handle file is required to USE the key, but the actual private bytes
    # still live on the YubiKey hardware — the handle is just a pointer.
    yellow "↻  resident-key enrollment failed; trying simpler non-resident form..."
    if grep -qiE "invalid format|cred prot|credentialProtection" /tmp/ssh-keygen-err 2>/dev/null; then
        yellow "    (cause: libfido2 / YubiKey firmware version mismatch — common, harmless)"
    fi
    rm -f "$KEY" "${KEY}.pub" 2>/dev/null
    if ssh-keygen -t ed25519-sk -f "$KEY" -C "aur-yubikey@$USER"; then
        green "✓  Non-resident YubiKey key created. Private bytes live on YubiKey."
        yellow "   Note: ${KEY##*/} (the on-disk handle) is required to USE this key."
        yellow "   It contains no secret material, but back it up if you reformat."
        return
    fi

    # Both failed.
    red "✗  ssh-keygen failed for both resident and non-resident keys."
    red "    Likely cause: libfido2 mismatch with YubiKey firmware."
    red "    Workarounds:"
    red "      - Update Homebrew openssh + libfido2:"
    red "          brew upgrade openssh libfido2"
    red "      - Or use a different OpenSSH (Homebrew's vs system):"
    red "          /opt/homebrew/bin/ssh-keygen -t ed25519-sk -f $KEY -C 'aur-yubikey@$USER'"
    red "      - Or pick option 2 (passphrase) on next run."
    red "    Last ssh-keygen error:"
    red "      $(cat /tmp/ssh-keygen-err 2>/dev/null | tail -5)"
    exit 1
}

generate_key_passphrase() {
    KEY="$HOME/.ssh/aur_ed25519"
    if [ -f "$KEY" ]; then
        yellow "↻  $KEY already exists — keeping it"
        return
    fi
    read -r -s -p "    Passphrase: " PASSPHRASE; echo
    ssh-keygen -t ed25519 -f "$KEY" -C "aur@$USER" -N "$PASSPHRASE"
    green "✓  Key generated (passphrase-protected)"

    if [ -n "$PASSPHRASE" ] && command -v op >/dev/null 2>&1; then
        if ask "    Save the passphrase to 1Password under 'AUR SSH Key'?"; then
            local vault
            vault=$(op vault list --format=json | jq -r '.[] | select(.name=="Private") | .name' 2>/dev/null || true)
            [ -z "$vault" ] && vault=$(op vault list --format=json | jq -r '.[0].name' 2>/dev/null || true)
            if [ -n "$vault" ]; then
                op item create \
                    --category "Secure Note" \
                    --title "AUR SSH Key" \
                    --vault "$vault" \
                    "passphrase[concealed]=$PASSPHRASE" \
                    "username=$(whoami)" \
                    "key_path=$KEY" \
                    "notes=Passphrase for the dedicated AUR ed25519 SSH key. Used by ssh aur@aur.archlinux.org for AUR package pushes." \
                    >/dev/null
                green "✓  Saved to 1Password vault '$vault' — retrieve with: op read 'op://$vault/AUR SSH Key/passphrase'"
            fi
        fi
    fi
}

generate_key_none() {
    KEY="$HOME/.ssh/aur_ed25519"
    if [ -f "$KEY" ]; then
        yellow "↻  $KEY already exists — keeping it"
        return
    fi
    ssh-keygen -t ed25519 -f "$KEY" -C "aur@$USER" -N ""
    green "✓  Key generated (no passphrase — beware: anyone with file access can use it)"
}

echo
echo "How should the AUR SSH key be protected?"
echo "  1) YubiKey  (FIDO2 ed25519-sk)  — private key on hardware, touch to use   [recommended]"
echo "  2) Passphrase                   — software key, encrypted at rest, optionally saved to 1Password"
echo "  3) None                         — software key, no protection (fastest, least safe)"
read -r -p "Choose [1/2/3] (default 1): " choice
case "${choice:-1}" in
    1) generate_key_yubikey ;;
    2) generate_key_passphrase ;;
    3) generate_key_none ;;
    *) red "✗ invalid choice"; exit 1 ;;
esac

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

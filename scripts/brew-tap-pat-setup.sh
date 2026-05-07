#!/usr/bin/env bash
# Helper to set the HOMEBREW_TAP_GITHUB_TOKEN secret on j4y-w4lk3r/pikvm.
#
# What you still have to do manually (GitHub doesn't let CLIs create
# fine-grained PATs):
#   1. Open the PAT creation page in your browser
#   2. Configure: scope = j4y-w4lk3r/homebrew-pikvm only,
#      Contents = Read and write
#   3. Click "Generate token" and copy it
#
# This script then:
#   4. Optionally saves the PAT to 1Password ("Homebrew Tap PAT")
#   5. Sets it as the HOMEBREW_TAP_GITHUB_TOKEN secret on this repo
#      via `gh secret set` (no web UI clicks)
#   6. Verifies the secret is registered
#
# Run from anywhere:
#   bash scripts/brew-tap-pat-setup.sh

set -euo pipefail

REPO="j4y-w4lk3r/pikvm"
TAP_REPO="j4y-w4lk3r/homebrew-pikvm"
SECRET_NAME="HOMEBREW_TAP_GITHUB_TOKEN"
PAT_PAGE="https://github.com/settings/personal-access-tokens/new"

green() { printf '\033[0;32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[1;33m%s\033[0m\n' "$*"; }
red() { printf '\033[0;31m%s\033[0m\n' "$*" >&2; }
ask() { read -r -p "$1 [y/N]: " ans; [[ "$ans" =~ ^[Yy] ]]; }

# 0. sanity
command -v gh >/dev/null 2>&1 || { red "gh CLI not in PATH. Install: brew install gh"; exit 1; }
gh api user --jq .login >/dev/null 2>&1 || { red "gh not authenticated. Run: gid login"; exit 1; }

current_user=$(gh api user --jq .login)
if [ "$current_user" != "j4y-w4lk3r" ]; then
    yellow "⚠  gh is currently authed as '$current_user', not j4y-w4lk3r."
    yellow "    The secret will be set on $REPO using $current_user's token,"
    yellow "    which is fine if that account has admin on the repo. Continuing."
fi

# 1. Browser flow
green "→  Opening PAT creation page..."
echo "    $PAT_PAGE"
echo
echo "Configure the PAT exactly like this:"
cat <<EOF
   ┌──────────────────────────────────────────────────────────────┐
   │ Token name           │ homebrew-pikvm-write                  │
   │ Resource owner       │ j4y-w4lk3r                            │
   │ Repository access    │ Only select repositories →            │
   │                      │ $TAP_REPO                              │
   │ Repository perms     │ Contents → Read and write             │
   │ Expiration           │ 1 year (or shorter if you prefer)     │
   └──────────────────────────────────────────────────────────────┘
EOF
echo

if command -v open >/dev/null 2>&1; then
    open "$PAT_PAGE" >/dev/null 2>&1 || true
elif command -v xdg-open >/dev/null 2>&1; then
    xdg-open "$PAT_PAGE" >/dev/null 2>&1 || true
fi

read -r -s -p "Paste the generated PAT (hidden input): " PAT
echo
if [ -z "$PAT" ]; then
    red "✗  no PAT provided, aborting"; exit 1
fi
if [[ ! "$PAT" =~ ^(github_pat_|ghp_) ]]; then
    yellow "⚠  PAT doesn't start with github_pat_ or ghp_ — double-check you pasted correctly"
    if ! ask "    Continue anyway?"; then exit 1; fi
fi

# 2. Optional: save to 1Password
if command -v op >/dev/null 2>&1; then
    if ask "→  Save the PAT to 1Password under 'Homebrew Tap PAT'?"; then
        # Use the default Private vault if it exists, else first vault.
        VAULT=$(op vault list --format=json | jq -r '.[] | select(.name=="Private") | .name' 2>/dev/null || true)
        [ -z "$VAULT" ] && VAULT=$(op vault list --format=json | jq -r '.[0].name' 2>/dev/null || true)
        [ -z "$VAULT" ] && { red "✗  no 1Password vaults available; skipping save"; }
        if [ -n "$VAULT" ]; then
            op item create \
                --category "API Credential" \
                --title "Homebrew Tap PAT" \
                --vault "$VAULT" \
                "credential[concealed]=$PAT" \
                "username=j4y-w4lk3r" \
                "url=https://github.com/$TAP_REPO" \
                "notes=Fine-grained GitHub PAT for goreleaser to push Formula/pikvm.rb to $TAP_REPO. Saved as $SECRET_NAME on $REPO." \
                >/dev/null
            green "✓  Saved to 1Password vault '$VAULT' as 'Homebrew Tap PAT'"
            green "    Retrieve later with: op read 'op://$VAULT/Homebrew Tap PAT/credential'"
        fi
    fi
fi

# 3. Set the secret on the repo
green "→  Setting $SECRET_NAME on $REPO via gh CLI..."
printf '%s' "$PAT" | gh secret set "$SECRET_NAME" --repo "$REPO" --body -
green "✓  Secret stored"

# 4. Verify
green "→  Verifying..."
if gh secret list --repo "$REPO" | grep -q "^$SECRET_NAME"; then
    green "✓  $SECRET_NAME is registered on $REPO"
    gh secret list --repo "$REPO" | grep "^$SECRET_NAME" || true
else
    red "✗  Couldn't confirm $SECRET_NAME exists. Check manually:"
    red "    https://github.com/$REPO/settings/secrets/actions"
    exit 1
fi

cat <<EOF

$(green "Done.") Next steps:
  1. (If not done yet) Set up AUR account + SSH key:
        bash arch/aur-setup.sh
  2. Cut the first release:
        cd ~/px/pikvm
        git tag v0.1.0
        git push origin v0.1.0
  3. After release artifacts upload, bump AUR:
        make aur-bump VER=0.1.0

EOF

# Releasing

This repo ships pre-built binaries for `pikvm` (macOS arm64/amd64, Linux arm64/amd64) plus three packaging tracks so installation is one command on every platform we care about:

| Platform | Install command |
|---|---|
| macOS + Linux (Homebrew) | `brew install j4y-w4lk3r/pikvm/pikvm` |
| Arch Linux (AUR) | `yay -S pikvm-bin` |
| Debian / Ubuntu | `sudo dpkg -i pikvm_<ver>_linux_amd64.deb` |
| Fedora / RHEL | `sudo rpm -i pikvm-<ver>-1.x86_64.rpm` |
| Anything else | download the `.tar.gz`, extract, move binary to `PATH` |

Most of that is automated by GoReleaser + GitHub Actions on every `vX.Y.Z` tag. There are some one-time setup steps before the very first release; subsequent releases are a single tag push.

## One-time setup

### 1. Create the Homebrew tap repo

GoReleaser commits `Formula/pikvm.rb` to a separate "tap" repo on every release. That repo must already exist:

```bash
gh repo create j4y-w4lk3r/homebrew-pikvm --public \
    --description "Homebrew tap for pikvm"
```

Empty is fine — the formula is auto-pushed during the first release.

### 2. Give GoReleaser a PAT for the tap repo

`GITHUB_TOKEN` (auto-provided to Actions) can write to **this** repo only. To also push commits to `homebrew-pikvm`, create a fine-grained PAT scoped to that repo with `Contents: Read and write`. Save it as a repo secret here:

```
Settings → Secrets and variables → Actions → New repository secret
Name:  HOMEBREW_TAP_GITHUB_TOKEN
Value: <PAT>
```

### 3. Set up an AUR account + SSH key (one-time)

The AUR is its own git host (not GitHub). It uses SSH for authenticated pushes.

1. Register an account at <https://aur.archlinux.org/register> (free; choose any username).
2. On your Mac, generate an SSH key dedicated to AUR if you don't already have one:
   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/aur_ed25519 -C "aur@$USER"
   ```
3. Copy the public key into your AUR account: My Account → SSH Public Key → paste `~/.ssh/aur_ed25519.pub`.
4. Tell SSH to use it for the AUR host. Append to `~/.ssh/config`:
   ```
   Host aur.archlinux.org
       IdentityFile ~/.ssh/aur_ed25519
       User aur
   ```
5. Smoke-test:
   ```bash
   ssh aur@aur.archlinux.org help
   # → "Welcome to the AUR..." means it's working.
   ```

### 4. Bootstrap the `pikvm-bin` AUR package

This is a one-time push to create the package on the AUR. After this, releases just bump `pkgver` automatically.

```bash
# Clone the (empty) AUR repo for pikvm-bin
git clone ssh://aur@aur.archlinux.org/pikvm-bin.git /tmp/pikvm-aur
cp arch/PKGBUILD arch/.SRCINFO /tmp/pikvm-aur/
cd /tmp/pikvm-aur
git add PKGBUILD .SRCINFO
git commit -m "Initial import: pikvm-bin"
git push origin master
```

After this, `aur.archlinux.org/packages/pikvm-bin` exists and any user can `yay -S pikvm-bin`.

## Cutting a release

Once all three setup steps are done, every release is two commands:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The `release.yml` GitHub Action runs GoReleaser, which:

1. Cross-compiles for darwin amd64/arm64 and linux amd64/arm64
2. Archives each as `pikvm_<version>_<os>_<arch>.tar.gz`
3. Builds `.deb`, `.rpm`, and `.pkg.tar.zst` packages from the same binaries
4. Creates a GitHub release on this repo with the generated changelog
5. Commits `Formula/pikvm.rb` into `j4y-w4lk3r/homebrew-pikvm`

The Homebrew tap is now usable. **The AUR package still needs a manual update** for every release (see next section).

### After each release: bump the AUR pkgver

GitHub Actions can't push to the AUR (the SSH key would have to live in CI, which is risky). Easiest is a tiny local script — run after `git push origin v0.1.0`:

```bash
make aur-bump VER=0.1.0
```

That target lives in the Makefile; under the hood it:
1. Clones `ssh://aur@aur.archlinux.org/pikvm-bin.git`
2. Updates `pkgver=` and the source SHA256s using the freshly-uploaded GitHub release tarballs
3. Regenerates `.SRCINFO`
4. Commits + pushes back

If you'd rather do it later we can wire up a separate GitHub Action that uses an AUR-scoped SSH key from secrets.

## Testing locally (no tag push, no GitHub writes)

```bash
make snapshot
```

produces `./dist/` with all 4 tarballs + a `.deb` + `.rpm` + `.pkg.tar.zst`. Inspect before tagging.

## What the end user sees

```bash
$ brew install j4y-w4lk3r/pikvm/pikvm           # macOS / Linux Homebrew
$ yay -S pikvm-bin                              # Arch / Manjaro
$ sudo dpkg -i pikvm_<ver>_linux_amd64.deb      # Debian / Ubuntu
$ sudo rpm -i pikvm-<ver>-1.x86_64.rpm          # Fedora / RHEL

$ pikvm                                         # TUI from any directory
$ brew upgrade pikvm                            # update via Homebrew when new tag lands
$ yay -Syu                                      # update via AUR
```

On first launch, `pikvm` needs a `config.json` or `.env` next to the binary (or in cwd). The Homebrew/AUR/deb/rpm paths put the binary at `/usr/bin/pikvm` or `/opt/homebrew/bin/pikvm`, so the typical setup is:

```bash
mkdir -p ~/.config/pikvm
cd ~/.config/pikvm
cp ~/path/to/config.json .
pikvm
```

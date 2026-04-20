# Releasing

This repo ships pre-built binaries for `pikvm` (macOS arm64/amd64, Linux arm64/amd64) + a Homebrew tap so end users can install with:

```bash
brew install lso0/pikvm/pikvm
```

All of that is automated — you just push a tag. But there are two one-time bits of setup before the very first release.

## One-time setup

### 1. Create the Homebrew tap repo

Create a new GitHub repo named exactly `homebrew-pikvm` under the same owner that hosts this repo (`lso0`):

```bash
gh repo create lso0/homebrew-pikvm --public --description "Homebrew tap for pikvm"
```

It can be empty — goreleaser will commit `Formula/pikvm.rb` on every release.

### 2. Give goreleaser a PAT for that tap repo

`GITHUB_TOKEN` (auto-provided to Actions) can only write to **this** repo. To also push commits to `homebrew-pikvm`, create a Personal Access Token with `contents: write` on that repo and save it as a repository secret named `HOMEBREW_TAP_GITHUB_TOKEN`:

```
Settings → Secrets and variables → Actions → New repository secret
Name:  HOMEBREW_TAP_GITHUB_TOKEN
Value: <PAT with Contents: Write on homebrew-pikvm>
```

## Cutting a release

Once the above is done, every release is three commands:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The `release.yml` GitHub Action runs goreleaser which:

1. Cross-compiles for darwin amd64/arm64 and linux amd64/arm64
2. Archives each as `pikvm_<version>_<os>_<arch>.tar.gz` with checksums
3. Creates a GitHub release on this repo with the generated changelog
4. Commits `Formula/pikvm.rb` into `lso0/homebrew-pikvm`

## Testing locally (no tag push, no GitHub writes)

```bash
make snapshot
```

produces `./dist/` with all 4 binaries + tarballs. Inspect before tagging.

## What the end user sees

```bash
$ brew tap lso0/pikvm                  # one-time per machine
$ brew install pikvm
$ pikvm                                # TUI from any directory
$ brew upgrade pikvm                   # updates via brew when new tag lands
$ brew uninstall pikvm
```

On first launch, `pikvm` needs a `config.json` or `.env` next to the binary (or in the current working directory). `brew install` puts the binary at `/opt/homebrew/bin/pikvm`, so the typical setup is:

```bash
cd ~/.config/pikvm
cp ~/path/to/config.json .
cd ~/.config/pikvm && pikvm     # config.json picked up from cwd
```

…or just keep using `~/px/pikvm` and `./pikvm` from source.

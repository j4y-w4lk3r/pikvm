# mm0 — Mac Mini M4 iOS dev lab setup plan

**Target machine:** Mac mini (2024), Apple M4, 32 GB RAM, 10 GbE, 500 GB SSD  
**Target identity:** `j4y@mm0` (Short Name / Local Hostname: `mm0`, Computer Name: human-friendly e.g. `Mac mini mm0`)  
**Primary use:** iOS development — many concurrent simulators, fast Xcode builds, CLI tools, sim screenshots/screen recordings over 2.5/10 Gb LAN to NAS/workstation  
**Orchestration:** PiKVM (`pikvm1`) + NAS + 1Password + Tailscale/Headscale  
**Status:** Phase 2 software done (2026-08-16) — ESP32 power + session v1 tested; Phase 0 still required before wipe  

---

## Table of contents

1. [Goals and non-goals](#1-goals-and-non-goals)
2. [Hardware and network topology](#2-hardware-and-network-topology)
3. [What you are not asking (but should decide)](#3-what-you-are-not-asking-but-should-decide)
4. [Architecture overview](#4-architecture-overview)
5. [Phase 0 — Preserve the current Mac mini (before any wipe)](#phase-0--preserve-the-current-mac-mini-before-any-wipe)
6. [Phase 1 — Lab infrastructure prep](#phase-1--lab-infrastructure-prep)
7. [Phase 2 — PiKVM software: power, profile, recording](#phase-2--pikvm-software-power-profile-recording)
8. [Phase 3 — 1Password secret layout](#phase-3--1password-secret-layout)
9. [Phase 4 — First boot & Setup Assistant (PiKVM-driven)](#phase-4--first-boot--setup-assistant-pikvm-driven)
10. [Phase 5 — Post-install (existing macOS setup repo)](#phase-5--post-install-existing-macos-setup-repo)
11. [Phase 6 — iOS / Xcode / simulator tuning](#phase-6--ios--xcode--simulator-tuning)
12. [Phase 7 — Tailscale, SSH, remote ops](#phase-7--tailscale-ssh-remote-ops)
13. [Phase 8 — Simulator capture pipeline (screenshots & screen recordings)](#phase-8--simulator-capture-pipeline-screenshots--screen-recordings)
14. [PiKVM feature backlog tied to this project](#pikvm-feature-backlog-tied-to-this-project)
15. [Risks, limits, and honest expectations](#risks-limits-and-honest-expectations)
16. [Success criteria](#success-criteria)
17. [Suggested timeline](#suggested-timeline)
18. [Open questions for j4y](#open-questions-for-j4y)

---

## 1. Goals and non-goals

### Goals

- [ ] Reproducible **from-first-power-on** setup of `mm0`, documented and **recorded** (video + optional HID/action log) to NAS
- [ ] Run orchestration from **NAS** (`pikvm` CLI/TUI against `pikvm1`, recordings under `/mnt/nas/pikvm-recordings` or NAS-local bu path)
- [ ] Create local user **`j4y`**, hostname **`mm0`**, store credentials in **1Password** (high-entropy password + separate Apple ID references)
- [ ] Install **Xcode + CLT**, configure for **heavy parallel simulator** workloads
- [ ] Join **tailnet** so `ssh j4y@mm0` works from any tailnet device
- [ ] Integrate **ESP32 power** (not PiKVM ATX) for Mac mini on/off
- [ ] Merge learnings from **existing post-install macOS setup** repo + **optimization backups** on current machine
- [ ] Optimize for **speed where it matters** (sim launch, derived data, network capture path), quality elsewhere (backups, logging, reproducibility)

### Non-goals (for v1)

- [ ] Fully unattended Apple ID login (2FA / device trust / “Sign in with Apple” prompts often need a human or pre-staged session)
- [ ] Replacing Apple Configurator / MDM for fleet enrollment (unless you later choose ABM + MDM)
- [ ] Running PiKVM capture at 4K120 on the Mac HDMI (Mac mini HDMI out is typically 4K60; PiKVM path is 1080p-class — sufficient for Setup Assistant automation)
- [ ] Storing Apple ID password in automation scripts (1Password `op read` at runtime only; never commit)

---

## 2. Hardware and network topology

```
┌─────────────────────────────────────────────────────────────────────────┐
│  j4y workstation (Arch, RTX 5080, Dell 4K)                               │
│  - PiKVM control / optional viewing                                      │
│  - Heavy dev optional                                                     │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │ Tailscale / 2.5G LAN
┌───────────────────────────────▼─────────────────────────────────────────┐
│  NAS (pikvm-bin installed, NFS bu mount)                                 │
│  - Run: pikvm --host pikvm1 …                                            │
│  - Recordings: …/pikvm-recordings/mm0-setup-YYYY-MM-DD/                  │
│  - Future: sim screenshot/rec ingest                                       │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
┌───────────────────────────────▼─────────────────────────────────────────┐
│  pikvm1 (100.64.0.8) — KVM switch extender 2                             │
│  Port 2.1 → Mac mini mm0 (HDMI + USB HID)                                │
│  Port 1.2 → router (existing profile)                                    │
│  Port 1.3 → nas (existing profile)                                       │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │ HDMI + USB
┌───────────────────────────────▼─────────────────────────────────────────┐
│  Mac mini M4 (mm0) — 10 GbE to switch                                    │
│  Power: ESP32 relay (NOT PiKVM ATX)                                      │
│    ON:  curl http://192.168.1.56/short                                   │
│    OFF: curl http://192.168.1.56/long                                    │
└─────────────────────────────────────────────────────────────────────────┘
```

| Item | Value |
|------|--------|
| PiKVM host | `pikvm1` |
| Switch port | **2.1** (extender 2, port 1) |
| PiKVM power | **None** — use ESP32 |
| ESP32 base URL | `http://192.168.1.56` |
| ESP32 power on | `GET /short` |
| ESP32 power off | `GET /long` |
| Target SSH | `j4y@mm0` (MagicDNS or tailnet IP) |
| Recordings root | NAS: `pikvm-recordings/` (see `config.recordings_dir`) |

**Note:** Add `2.1` profile to `~/.config/pikvm/state.json` once — do not commit secrets there, only names/tags/power backend.

---

## 3. What you are not asking (but should decide)

These decisions affect speed, security, and how automatable macOS setup really is.

### 3.1 macOS install source

| Option | Pros | Cons |
|--------|------|------|
| **A. Out-of-box Setup Assistant only** (factory macOS) | Simplest; PiKVM sees real OOBE | Apple ID / iCloud / Analytics screens vary by macOS version |
| **B. Recovery / reinstall macOS first** | Clean known version | Extra steps; needs network in Recovery |
| **C. IPSW + Apple Configurator restore** | Reproducible build | Needs second Mac or Configurator CLI; USB-C connection may bypass PiKVM HDMI for part of flow |

**Recommendation:** Start with **A** for v1 recording; document macOS version in session metadata. Consider **C** for true re-flash reproducibility later.

### 3.2 FileVault

- **On:** Better security; remote reboot needs password at preboot (bad for unattended PiKVM)
- **Off:** Easier remote power-cycle + auto-login for dev box; physical security assumed (homelab)

**Recommendation for iOS dev lab:** FileVault **off** initially (or use autologin + no FileVault), revisit if machine leaves trusted network.

### 3.3 Apple ID during setup

- Required for: Xcode downloads, simulator runtimes, App Store, some signing
- Pain points: 2FA, “Trust this device”, Terms updates, iCloud Drive opt-in screens

**Recommendation:** Store Apple ID in 1Password; plan **human-in-the-loop** for 2FA via phone/watch during first recording; automate typing via PiKVM HID where safe; never log Apple ID password in recordings repo.

### 3.4 Local vs Apple ID user

- Create **local account `j4y`** first (admin)
- Optionally link Apple ID later in System Settings — do **not** use “Sign in with Apple ID” as the *only* account if you want stable `j4y@mm0` SSH

### 3.4 Network during setup

- **Ethernet 10 GbE** preferred (already on your spec) — faster Xcode/runtime downloads
- Document whether setup uses ** DHCP reservation** for `mm0` → helps Tailscale and NAS ingest paths

### 3.5 Where post-install scripts live

You mentioned a **successful GitHub macOS setup (post-install)**. Before wipe:

- [ ] Identify exact repo URL + branch (e.g. `setup-0`, private script collection, or repo not yet pushed)
- [ ] Tag commit used for current working optimizations
- [ ] Import into a single **`mm0-setup`** or **`macos-dev-mm0`** repo if fragmented

Local optimization backups (current machine, user `wgmm0`):

```
/Users/wgmm0/macos-optimization-backup-20260112-215826
/Users/wgmm0/macos-optimization-backup-20260112-220052
/Users/wgmm0/macos-optimization-backup-20260112-220611
```

**Action:** Copy these to NAS **before Phase 4**, diff the three, merge into one canonical `optimize-mm0.sh` + plist set.

### 3.6 Simulator screenshot/rec “over 2.5 Gb network”

Clarify path:

- **Simulator runs on mm0** → capture locally → **push to NAS** (recommended; 10 GbE mm0 → NAS)
- vs **streaming framebuffer over network** (slow, rarely worth it)

**Recommendation:** `xcrun simctl io booted screenshot` / `recordVideo` + `rsync`/`rclone` to NAS watch folder; optional small **mm0 agent** (launchd) uploading on file change.

### 3.7 Power semantics (ESP32)

Confirm with hardware:

- Does `/short` = momentary press (wake / toggle) and `/long` = hold shutdown?
- Is ESP32 on **always**, reachable when Mac is off?
- Should `pikvm` enforce cooldown between power events (Mac needs time for NVRAM/sleep)?

Document in profile `notes` once verified.

---

## 4. Architecture overview

Three layers — do not skip layer 0:

```
┌──────────────────────────────────────────────────────────────┐
│  Layer 3 — Post-install (repeatable, mostly SSH)              │
│  GitHub macOS setup scripts, optimizations, Xcode, Tailscale  │
└──────────────────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────────────────┐
│  Layer 2 — Setup Assistant (PiKVM HID + human 2FA)          │
│  Recorded session → MP4 + future action JSON (roadmap #14)   │
└──────────────────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────────────────┐
│  Layer 1 — Power + routing (ESP32 + pikvm switch port 2.1)  │
└──────────────────────────────────────────────────────────────┘
```

**NAS as control plane:** SSH to NAS → run `pikvm record`, `pikvm power`, future `pikvm session record`. Keeps recordings off workstation and close to NFS bu.

---

## Phase 0 — Preserve the current Mac mini (before any wipe)

> **Do this first.** You asked to add the current Mac to Tailscale and extract the setup script before deleting anything.

- [ ] **0.1** Ensure current Mac is **on tailnet** (Tailscale app or `tailscale up` — auth key in 1Password)
  - [ ] Record tailnet name: `________________` (e.g. `mm0` or `wgmm0`)
  - [ ] Verify: `ssh j4y@<current-hostname>` from workstation/NAS
- [ ] **0.2** Export **GitHub macOS setup** repo to NAS (clone mirror if only on Mac)
- [ ] **0.3** Copy optimization backup dirs to NAS:
  ```bash
  # Run ON current Mac (adjust NAS mount)
  rsync -a /Users/wgmm0/macos-optimization-backup-* nas:/home/j4y/px/bu/mm0-optimization-backups/
  ```
- [ ] **0.4** Export list of installed Xcode version, simulator runtimes, global defaults:
  ```bash
  xcodebuild -version > xcode-version.txt
  xcrun simctl list runtimes > sim-runtimes.txt
  defaults read > defaults-dump.txt  # review for secrets before storing
  brew bundle dump --file=Brewfile  # if Homebrew used
  ```
- [ ] **0.5** Document current **ESP32** behavior (film `/short` and `/long` with phone note)
- [ ] **0.6** Create 1Password vault items (see Phase 3) — **generate new password for fresh mm0**, do not reuse leaked/old defaults
- [ ] **0.7** Optional: one full **Time Machine** or `asr`/disk image to NAS (500 GB — ensure space)

**Exit criteria:** Tailscale SSH works; NAS has optimization backups + setup scripts; 1Password items exist; you can rebuild without the old disk state.

---

## Phase 1 — Lab infrastructure prep

- [ ] **1.1** PiKVM switch to port **2.1** — verify HDMI shows Mac (powered via ESP32)
- [ ] **1.2** NAS: confirm `pikvm-bin` ≥ 0.3.4, `config.json` has `pikvm1`, `recordings_dir` points at bu path
- [ ] **1.3** NAS: test recording path:
  ```bash
  pikvm record --host pikvm1 --duration 10
  # expect file under pikvm-recordings/
  ```
- [ ] **1.4** Workstation: Dell + PiKVM mirror scripts (`~/.config/display/`) if viewing from desk while NAS records
- [ ] **1.5** DHCP/DNS: reserve IP for mm0 on 10 GbE; optional local DNS `mm0.lan`
- [ ] **1.6** Firewall: allow mm0 → NAS (NFS/SMB/SSH), mm0 → internet for Apple/Xcode CDN

---

## Phase 2 — PiKVM software: power, profile, recording

### 2.1 Port profile (`state.json`)

Add (no secrets):

```json
"2.1": {
  "name": "mm0",
  "tags": ["mac", "ios-dev", "m4"],
  "tailscale_name": "mm0",
  "ssh_user": "j4y",
  "notes": "Mac mini M4 2024 32GB 10GbE. Power via ESP32 http://192.168.1.56 short=on long=off. No ATX."
}
```

- [x] **2.1** Profile `mm0` on port 2.1 with ESP32 HTTP power URLs

### 2.2 ESP32 power backend (new pikvm feature)

Today `pikvm power on/off` uses PiKVM ATX API — **wrong for mm0**.

Proposed design:

```json
// state.json or config extension (future schema)
"power_backend": {
  "type": "http",
  "on_url": "http://192.168.1.56/short",
  "off_url": "http://192.168.1.56/long",
  "method": "GET",
  "cooldown_sec": 30
}
```

CLI:

```bash
pikvm power on mm0      # hits ESP32 on_url
pikvm power off mm0
pikvm power cycle mm0   # off → wait → on
```

- [x] **2.2** Per-profile `power` in `state.json` (`type: http`, URLs per action)
- [x] **2.3** `internal/api/power_http.go` + CLI wiring (`pikvm power click|long mm0`)
- [ ] **2.4** Hooks: `power-on` / `power-off` still fire for ESP32 path
- [x] **2.5** Tests with mock HTTP server

### 2.3 Recording modes

| Mode | Exists today | Needed for mm0 |
|------|----------------|----------------|
| `pikvm record --duration N` | ✅ MP4 to NAS | Use for each setup phase |
| **Session record** (video + HID log) | ❌ roadmap #14 | **Build** — start minimal v1 |
| Snapshot on demand | ✅ API / scripts | OCR checkpoints |

**Minimal session record v1 (recommended before full #14):**

- [x] Background MP4 in 10m chunks via detached `pikvm session worker` subprocess
- [x] Append HID events + notes to `actions.jsonl` (cross-process via `~/.config/pikvm/active-session.json`)
- [x] Session folder: `pikvm-recordings/sessions/YYYYMMDD-HHMMSS-mm0-<name>/`
  - `screen-NNN.mp4` (10-minute segments)
  - `actions.jsonl` `{ts, event, …}`
  - `meta.json` `{host, port, machine, name, started_at}`

- [x] **2.6** `pikvm session start mm0 --name setup-oobe` (tested 2026-08-16)
- [x] **2.7** `pikvm session stop` → SIGTERM worker + finalize log
- [ ] **2.8** NAS cron: sync sessions to long-term archive

---

## Phase 3 — 1Password secret layout

Create items in vault **`lab`** (or your PiKVM vault) — mirror `pi-setup` / `pikvm` patterns.

### Item: `mm0 — macOS local`

| Field | Content |
|-------|---------|
| username | `j4y` |
| password | **generated** — 32+ chars, alphanumeric + symbols (1Password generator) |
| hostname | `mm0` |
| notes | Created YYYY-MM-DD; FileVault off; admin |

### Item: `mm0 — Apple ID` (reference only)

| Field | Content |
|-------|---------|
| username | Apple ID email |
| password | (stored; never in git) |
| notes | 2FA device: iPhone / YubiKey — human step during setup |

### Item: `mm0 — Tailscale`

| Field | Content |
|-------|---------|
| auth key | reusable or one-shot key for `tailscale up` |
| hostname | `mm0` |
| tags | `tag:ios-dev` (if using ACL tags) |

### Item: `mm0 — ESP32 power`

| Field | Content |
|-------|---------|
| URL on | `http://192.168.1.56/short` |
| URL off | `http://192.168.1.56/long` |

### Optional: `mm0 — Xcode / signing`

- Apple Developer team ID, certificates location, `MATCH_PASSWORD`, etc. (Phase 6)

**Automation:**

```bash
op item create --category login --title 'mm0 — macOS local' ...
# pikvm / setup scripts: op read "op://lab/mm0 — macOS local/password"
```

- [ ] **3.1** Create items
- [ ] **3.2** Add `pikvm hosts`-style profile or `mm0-setup.env.op` template (gitignored)
- [ ] **3.3** Never store op:// paths with real vault IDs in public repos (use env or config.local.json)

---

## Phase 4 — First boot & Setup Assistant (PiKVM-driven)

Mac mini has **no BIOS**. Automation = **PiKVM USB HID keyboard/mouse** + **HDMI capture** + **timings** + **optional OCR** (roadmap #13/#22).

### 4.1 Pre-flight

- [ ] ESP32 power **off** (long press endpoint), wait 30s
- [ ] `pikvm switch 2.1` (or `pikvm switch mm0` once profile exists)
- [ ] `pikvm session start mm0 --name oobe-01`
- [ ] ESP32 power **on** (short)

### 4.2 Setup Assistant checklist (record every step)

Use PiKVM web UI or TUI to send keys; prefer **Tab / arrow / Enter** over mouse when possible.

- [ ] Language & region
- [ ] Country
- [ ] Accessibility — skip or configure once
- [ ] Network — **Ethernet** should auto-connect; verify 10 GbE link
- [ ] Data & Privacy / Analytics — choose consistent with dev box policy
- [ ] **Create user:** Full name `j4y`, Account name `j4y`, password from 1Password
- [ ] **Computer name:** `mm0` (verify `scutil --get LocalHostName` later)
- [ ] Enable Location — optional
- [ ] Siri — **Off** (saves RAM/noise on dev box)
- [ ] Screen Time — skip
- [ ] **Apple ID** — sign in (human 2FA); or **Set Up Later** then sign in after CLT
- [ ] iCloud Drive — your choice (off = less background sync; on = more convenience)
- [ ] Touch ID — N/A on Mac mini
- [ ] Terms & Conditions
- [ ] FileVault — **Off** (per decision above)
- [ ] Create computer account — done
- [ ] Express Setup vs Customize — **Customize** → disable unnecessary

### 4.3 Immediately after first login (still on PiKVM)

- [ ] System Settings → Sharing → **Remote Login** on
- [ ] Install Tailscale; join tailnet (auth key from 1Password)
- [ ] Verify `ssh j4y@mm0` from NAS — **switch primary control to SSH**
- [ ] `pikvm session stop` — save OOBE recording

### 4.4 macOS version pin

Record in `meta.json`: `ProductVersion`, `BuildVersion` (`sw_vers`).

---

## Phase 5 — Post-install (existing macOS setup repo)

After SSH works, PiKVM becomes **fallback only** (recovery, reinstall, stuck boot).

- [ ] **5.1** Clone/pull **GitHub macOS setup** repo on mm0
- [ ] **5.2** Merge **optimization backups** into repo as `scripts/optimize/` with README explaining each tweak
- [ ] **5.3** Run post-install idempotently:
  ```bash
  # Example pattern — adjust to your real repo
  ./scripts/install-dev-tools.sh
  ./scripts/optimize/simulator-ram.sh
  ./scripts/optimize/disable-spotlight-unnecessary.sh
  ```
- [ ] **5.4** Record **second session** (terminal-only via `script` or PiKVM if you want visual): post-install log to NAS
- [ ] **5.5** Tag git commit on setup repo: `mm0-baseline-YYYY-MM-DD`

**Repo hygiene suggestion:** one repo `macos-mm0-setup` with:

```
scripts/
  oobe/           # manual checklist + optional HID JSON sequences
  post/           # from your successful GitHub setup
  optimize/       # merged from macos-optimization-backup-*
  capture/        # sim screenshot upload helpers
docs/
  MM0-SETUP-PLAN.md  # link or copy of this file
```

---

## Phase 6 — iOS / Xcode / simulator tuning

Optimize for **many simulators** + **fast iteration** on 32 GB RAM.

### 6.1 Xcode & runtimes

- [ ] Install **Xcode** from App Store (signed in Apple ID)
- [ ] `xcode-select -s /Applications/Xcode.app/Contents/Developer`
- [ ] `xcodebuild -runFirstLaunch`
- [ ] Install iOS simulator runtimes needed (iOS 18.x + one older if you ship wide)
- [ ] Accept licenses: `sudo xcodebuild -license accept`

### 6.2 RAM / process tuning (from your optimization backups)

Review backups for patterns like:

- Disable unnecessary Login Items / LaunchAgents
- Reduce visual effects (Accessibility → Reduce motion/transparency) — minor RAM win
- **Simulator:** limit concurrent booted sims via workflow (e.g. max N per project)
- `defaults write` com.apple.dt.Xcode IDEParallelTestingEnabled — test parallelization
- Derived Data on local SSD (500 GB — monitor size; periodic clean)
- Optional: **Increased memory limit** for specific sims (Xcode 15+ per-sim setting)

- [ ] **6.2** Apply only **documented** optimizations with before/after notes
- [ ] **6.3** Benchmark: cold boot sim, build sample app, record RAM (`memory_pressure`, Activity Monitor)

### 6.3 CLI tools (examples)

- [ ] Homebrew (`/opt/homebrew`) — git, jq, ripgrep, fastlane, swiftlint, etc.
- [ ] Your existing CLI apps repos
- [ ] `pikvm` / `mm0-agent` not required on mm0 unless mm0 self-records

### 6.4 Code signing

- [ ] Apple Developer Program on this Apple ID
- [ ] Certificates: Xcode automatic signing for dev; document team ID in 1Password
- [ ] Fastlane Match optional for shared certs across machines

---

## Phase 7 — Tailscale, SSH, remote ops

- [ ] **7.1** Install Tailscale on mm0 (`tailscale up --auth-key=… --hostname=mm0`)
- [ ] **7.2** Headscale ACL: allow `j4y` → `mm0:22`, dev machines → mm0 for CI if needed
- [ ] **7.3** SSH: key-based auth; disable password SSH after keys work
- [ ] **7.4** Optional: `pikvm ssh mm0` integration (already in roadmap #11 — wire `tailscale_name` + `ssh_user` in profile)
- [ ] **7.5** mosh for flaky links optional
- [ ] **7.6** Register in **`hs`** TUI tailnet tool (`px/x-j4y/hs`) if you use multi-tailnet switching

**Order you suggested:** join **current** Mac to tailnet **before** wipe → extract scripts → then rebuild mm0 clean — **correct**.

---

## Phase 8 — Simulator capture pipeline (screenshots & screen recordings)

Goal: fast sim output on NAS for review from any device on 2.5/10 Gb LAN.

### 8.1 Local capture on mm0

```bash
# Screenshot
xcrun simctl io booted screenshot /tmp/sim.png

# Video (iOS 17+)
xcrun simctl io booted recordVideo /tmp/sim.mov
```

### 8.2 Upload to NAS

```bash
rsync -av /tmp/sim.png nas:/home/j4y/px/bu/sim-captures/mm0/$(date +%Y%m%d-%H%M%S).png
```

- [ ] **8.1** `scripts/capture/sim-shot.sh` + `--upload`
- [ ] **8.2** Optional **launchd** watcher on `~/sim-output/` → auto-rsync
- [ ] **8.3** Optional integration with **quick-capture-app** repo for window targeting

### 8.3 Network path

Prefer: **mm0 (10 GbE) → switch → NAS (2.5/10 GbE)** — not PiKVM HDMI path (HDMI is for setup/recovery only).

---

## PiKVM feature backlog tied to this project

Priority order for **mm0** specifically:

| Priority | Feature | Roadmap | Notes |
|----------|---------|---------|-------|
| P0 | ESP32 HTTP power backend | new | Blocks `pikvm power mm0` |
| P0 | Port profile `2.1` / `mm0` | #11 done | Add power backend field |
| P1 | Session record (MP4 + JSONL) | #14 partial | Minimal before OOBE |
| P1 | `recordings_dir` session subdirs | done | Organize by machine/session |
| P2 | OCR snapshot (`tesseract`) | #13 | Setup Assistant screen detection |
| P2 | JSON sequence replay | #12 | Replay OOBE after first manual run |
| P3 | AI screen classification | #22 | Apple ID dialogs, “Trust” prompts |
| P3 | NAS-side `pikvm session` daemon | new | Long recordings without laptop |

---

## Risks, limits, and honest expectations

1. **macOS Setup Assistant is not BIOS** — no F7 spam; timing + vision or human assistance required.
2. **Apple ID / 2FA** — plan for human presence during first setup recording.
3. **PiKVM HID** — mouse coordinates are fragile; prefer keyboard navigation.
4. **ESP32 power** — verify Mac mini responds correctly; Apple silicon Macs need proper shutdown (`osascript -e 'tell app "System Events" to shut down'` over SSH before ESP32 off).
5. **500 GB SSD** — Xcode + multiple runtimes + Derived Data fills quickly; budget **200+ GB** for Xcode alone; plan cleanup cron.
6. **Secrets in recordings** — MP4 may show passwords on screen; store recordings **private** on NAS; consider blurring password fields in published docs.
7. **Git history** — never commit `.env`, `config.json`, Apple passwords (same rules as pikvm repo).

---

## Success criteria

- [ ] `ssh j4y@mm0` from tailnet works
- [ ] Xcode builds and runs **≥3 simulators concurrently** without unacceptable swap
- [ ] Full OOBE + post-install recorded to NAS (`pikvm-recordings/sessions/mm0-setup-*`)
- [ ] 1Password holds mm0 credentials; nothing sensitive in git
- [ ] `pikvm power on|off mm0` controls ESP32 reliably
- [ ] Post-install repo tagged; optimization scripts merged and documented
- [ ] Sim screenshot lands on NAS in &lt;2 s after script completes (10 GbE path)
- [ ] You can **reinstall from scratch** following this doc + recordings in &lt;1 working day

---

## Suggested timeline

| Week | Focus |
|------|--------|
| W0 | Phase 0 — tailnet current Mac, backup scripts, 1Password items |
| W1 | Phase 1–2 — ESP32 power in pikvm, session record MVP, profile `mm0` |
| W2 | Phase 4 — OOBE recording (expect 1–2 manual-assisted runs) |
| W3 | Phase 5–7 — post-install, Xcode, Tailscale, SSH hardening |
| W4 | Phase 6 & 8 — simulator tuning, capture pipeline, benchmarks |

Adjust if Apple ID / Xcode download bottlenecks network.

---

## Open questions for j4y

Fill these in before Phase 4:

1. **Exact GitHub repo URL** for the successful post-install macOS setup?
2. **Tailnet hostname** for current Mac before wipe (`wgmm0`?)? Same MagicDNS name after rebuild or new?
3. **ESP32 wiring** — `/short` and `/long` confirmed as pulse vs hold? Safe to call when Mac already on?
4. **Apple ID strategy** — sign in during OOBE or defer until post-install SSH?
5. **FileVault** — off for dev lab?
6. **macOS fresh install** — factory OOBE vs Recovery reinstall vs IPSW?
7. **NAS control** — always run `pikvm` from NAS SSH, or also from workstation?
8. **Sim capture** — manual scripts enough, or build always-on watcher service?
9. **Multiple mm machines later** (`mm1`, …)? — if yes, generalize profile schema now.

---

*Document version: 2026-08-16 · maintained in `pikvm/docs/MM0-SETUP-PLAN.md`*

---

## Appendix A — Assets on NAS (2026-08-16)

Mac mini scripts/backups copied from `wgmm0@100.64.0.9` (tailnet host `wgmm0-1`):

| Path | Contents |
|------|----------|
| `/home/j4y/px/bu/mm0-setup/optimize-mac-mini-cicd.sh` | Main post-install optimization script |
| `/home/j4y/px/bu/mm0-setup/backups/215826/` | First optimization backup snapshot |
| `/home/j4y/px/bu/mm0-setup/backups/220052/` | Second snapshot (+ restore.sh) |
| `/home/j4y/px/bu/mm0-setup/backups/220611/` | Third snapshot (+ restore.sh) |

Desktop NFS mirror: `/mnt/nas/pikvm-recordings` when mounted.

## Appendix B — PiKVM commands (implemented 2026-08-16)

```bash
# ESP32 power (profile mm0 on port 2.1)
pikvm --host pikvm1 power click mm0
pikvm --host pikvm1 power long mm0    # off
pikvm --host pikvm1 power click mm0   # on (short pulse)

# Session recording to NAS (detached worker; state in ~/.config/pikvm/active-session.json)
pikvm --host pikvm1 session start mm0 --name setup-oobe
pikvm --host pikvm1 session note "Language selected"
pikvm --host pikvm1 session status
pikvm --host pikvm1 session stop

# Profile
pikvm --host pikvm1 profile get mm0
```

Profile `power` object: `type`, `on_url`, `off_url`, `click_url`, `long_url`, optional `method`, `cooldown_sec`.

Dev binary: `/tmp/pikvm` (built from repo; install or tag v0.3.5 when ready).

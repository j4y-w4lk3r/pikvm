# Example Hooks

Drop any of these into `~/.config/pikvm/hooks.d/` (or copy + adapt) to
react to live PiKVM events. See `pikvm hooks list` and `pikvm hooks test`
for management commands.

## Quick install one of these

```bash
mkdir -p ~/.config/pikvm/hooks.d
cp hooks-examples/iso-upload-finished.sh ~/.config/pikvm/hooks.d/
chmod +x ~/.config/pikvm/hooks.d/iso-upload-finished.sh

# Verify it would fire:
pikvm hooks list

# Synthetically trigger to test before wiring anything else up:
pikvm hooks test iso-upload-finished name=ubuntu-25.10.iso host_name=lab
```

## Filename → event matching

| Filename | Fires on |
|---|---|
| `port-changed.sh` | every active-port switch (any extension is allowed) |
| `power-on.py`     | any port powering on |
| `_all.sh`         | every event (catch-all) |
| `port-changed.d/notify-slack` | nested directory variant — same effect as `port-changed.sh` |

## Available events

| Event | Fires when |
|---|---|
| `host-connected` | the WebSocket connects to the active PiKVM |
| `host-disconnected` | WebSocket drops (auto-reconnect still tries) |
| `port-changed` | the active port switches (manual or external) |
| `power-on` / `power-off` | any port's ATX power state flips |
| `msd-mounted` / `msd-unmounted` | the virtual USB drive attaches/detaches |
| `iso-upload-finished` | an ISO upload (TUI or `pikvm.sh`) completes |
| `clients-changed` | the count of connected web/TUI/CLI clients changes |

## Environment variables

Every hook receives at least:

```
PIKVM_EVENT       e.g. "port-changed"
PIKVM_TIMESTAMP   RFC3339 UTC
PIKVM_HOST_NAME   "lab" / "garage" / "default"
PIKVM_HOST        "100.64.183.14"
PIKVM_USER        connection username
```

Plus event-specific keys (all uppercased and prefixed with `PIKVM_`):

| Event | Extra vars |
|---|---|
| `port-changed` | `PIKVM_PORT`, `PIKVM_PREV_PORT`, `PIKVM_NAME`, `PIKVM_PREV_NAME`, `PIKVM_PORT_ID` |
| `power-on` / `power-off` | `PIKVM_PORT`, `PIKVM_PORT_ID`, `PIKVM_NAME` |
| `msd-mounted` / `msd-unmounted` | `PIKVM_ONLINE`, `PIKVM_CONNECTED` |
| `iso-upload-finished` | `PIKVM_NAME`, `PIKVM_CONNECTED` |
| `clients-changed` | `PIKVM_COUNT`, `PIKVM_PREV_COUNT` |
| `host-disconnected` | `PIKVM_ERROR` (when set) |

The parent process's environment is also inherited, so hooks can rely
on `PATH`, `HOME`, `SLACK_WEBHOOK`, 1Password CLI sessions, etc.

## Logging + debugging

All hook stdout + stderr is appended to `~/.config/pikvm/hooks.log`
with timestamped headers. Tail it with:

```bash
pikvm hooks logs              # last 50 entries
pikvm hooks logs 200          # last 200 entries
tail -f ~/.config/pikvm/hooks.log
```

Each hook is given 30 seconds to finish; runaway hooks are SIGKILLed.

## Caveats

- Hooks are run **only** by the live TUI (`pikvm` with no args) and by
  `pikvm hooks test`. CLI one-shots (e.g. `pikvm power on j4yn0`) do
  not fire hooks themselves; the WebSocket subscriber in another running
  TUI will pick the resulting event up and dispatch from there.
- Hooks run with your normal user privileges. Don't put `rm -rf` in
  one without testing via `pikvm hooks test` first.

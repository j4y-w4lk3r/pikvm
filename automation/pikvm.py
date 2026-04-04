#!/usr/bin/env python3
"""
PiKVM tools: ATX control, ISO upload, and local-screen automation (PyAutoGUI).
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from base64 import b64encode
from datetime import datetime
from pathlib import Path


def load_env() -> dict[str, str]:
    """Load PIKVM_* from .env (prefer repo root, fall back to script dir)."""
    script_dir = Path(__file__).resolve().parent
    # When pikvm.py lives under automation/, .env is usually one directory up.
    candidates = [
        script_dir.parent / ".env",
        script_dir / ".env",
    ]
    env_file: Path | None = None
    for c in candidates:
        if c.is_file():
            env_file = c
            break
    if env_file is None:
        print("❌ Error: .env file not found (looked in: {})".format(", ".join(str(c) for c in candidates)))
        sys.exit(1)

    config: dict[str, str] = {}
    with open(env_file, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" in line:
                key, value = line.split("=", 1)
                config[key.strip()] = value.strip()

    required_keys = ("PIKVM_HOST", "PIKVM_USER", "PIKVM_PASS")
    for key in required_keys:
        if key not in config:
            print(f"❌ Error: {key} not found in .env file")
            sys.exit(1)

    return config


def _base_url(config: dict[str, str]) -> str:
    return f"https://{config['PIKVM_HOST']}/api"


# --- ATX control ---


def _requests():
    import requests
    from urllib3.exceptions import InsecureRequestWarning

    requests.packages.urllib3.disable_warnings(category=InsecureRequestWarning)
    return requests


class PiKVMController:
    def __init__(self, config: dict[str, str]):
        self._requests = _requests()
        self.auth = (config["PIKVM_USER"], config["PIKVM_PASS"])
        self.base_url = _base_url(config)

    def get_status(self, port: int = 0) -> dict:
        try:
            url = f"{self.base_url}/switch/atx/state?port={port}"
            response = self._requests.get(url, auth=self.auth, verify=False, timeout=5)
            if response.status_code == 200:
                data = response.json()
                return data.get("result", {})
            return {"status": "unavailable", "code": response.status_code}
        except Exception as e:
            return {"error": str(e)}

    def power_action(self, port: int = 0, action: str = "on") -> dict:
        try:
            url = f"{self.base_url}/switch/atx/power?port={port}&action={action}"
            response = self._requests.post(url, auth=self.auth, verify=False, timeout=5)
            return response.json()
        except Exception as e:
            return {"error": str(e)}

    def click_button(self, port: int = 0, button: str = "power") -> dict:
        try:
            url = f"{self.base_url}/switch/atx/click?port={port}&button={button}"
            response = self._requests.post(url, auth=self.auth, verify=False, timeout=5)
            return response.json()
        except Exception as e:
            return {"error": str(e)}


def _clear_screen() -> None:
    os.system("clear" if os.name != "nt" else "cls")


def _display_menu(controller: PiKVMController) -> None:
    _clear_screen()
    print("=" * 50)
    print("       PiKVM ATX Power Control")
    print("=" * 50)
    print()

    status = controller.get_status(port=0)
    if status and "error" not in status and "status" not in status:
        print("📊 Current Status:")
        print(f"   Power: {'🟢 ON' if status.get('leds', {}).get('power', False) else '🔴 OFF'}")
        print(f"   HDD:   {'🟢 ON' if status.get('leds', {}).get('hdd', False) else '🔴 OFF'}")
    else:
        print("📊 PiKVM Connection: Active")

    print()
    print("-" * 50)
    print("Actions:")
    print()
    print("  [1] Power ON")
    print("  [2] Power OFF")
    print("  [3] Power Click (short press)")
    print("  [4] Power Long Press")
    print("  [5] Reset Click (short press)")
    print("  [6] Reset Long Press")
    print("  [r] Refresh Status")
    print("  [q] Quit")
    print()
    print("-" * 50)


def run_control() -> int:
    config = load_env()
    controller = PiKVMController(config)

    while True:
        _display_menu(controller)
        choice = input("\nSelect action: ").strip().lower()

        if choice == "q":
            print("\n👋 Goodbye!")
            return 0

        if choice == "r":
            continue

        if choice == "1":
            print("\n⚡ Turning power ON...")
            result = controller.power_action(port=0, action="on")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        elif choice == "2":
            print("\n⚡ Turning power OFF...")
            result = controller.power_action(port=0, action="off")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        elif choice == "3":
            print("\n🔘 Power Click (short press)...")
            result = controller.click_button(port=0, button="power")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        elif choice == "4":
            print("\n🔘 Power Long Press...")
            result = controller.click_button(port=0, button="power_long")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        elif choice == "5":
            print("\n🔄 Reset Click (short press)...")
            result = controller.click_button(port=0, button="reset")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        elif choice == "6":
            print("\n🔄 Reset Long Press...")
            result = controller.click_button(port=0, button="reset_long")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        else:
            print("\n❌ Invalid choice")
            input("Press Enter to continue...")


# --- Upload: stdlib (urllib) ---


class _ProgressFileWrapper:
    def __init__(self, file_obj, total_size: int):
        self.file = file_obj
        self.total_size = total_size
        self.bytes_read = 0
        self.last_percent = -1

    def read(self, size: int = -1):
        chunk = self.file.read(size)
        if chunk:
            self.bytes_read += len(chunk)
            percent = int((self.bytes_read / self.total_size) * 100)
            if percent != self.last_percent and percent % 5 == 0:
                mb_read = self.bytes_read // (1024 * 1024)
                mb_total = self.total_size // (1024 * 1024)
                print(f"  📊 Progress: {mb_read} MB / {mb_total} MB ({percent}%)", flush=True)
                self.last_percent = percent
        return chunk


def upload_iso_builtin(iso_path: str, config: dict[str, str]) -> bool:
    if not os.path.exists(iso_path):
        print(f"❌ Error: File not found: {iso_path}")
        return False

    filename = os.path.basename(iso_path)
    file_size = os.path.getsize(iso_path)
    file_size_mb = file_size // (1024 * 1024)

    host = config["PIKVM_HOST"]
    user = config["PIKVM_USER"]
    password = config["PIKVM_PASS"]

    encoded_filename = urllib.parse.quote(filename)
    url = f"https://{host}/api/msd/write?image={encoded_filename}"

    print(f"📀 Uploading {filename}")
    print(f"   Size: {file_size_mb} MB")
    print(f"   Host: {host}")
    print("")
    print("⏳ Starting streaming upload (stdlib / urllib)...")
    print("")

    try:
        ssl_context = ssl._create_unverified_context()
        credentials = f"{user}:{password}"
        auth_string = b64encode(credentials.encode()).decode()

        with open(iso_path, "rb") as f:
            progress_file = _ProgressFileWrapper(f, file_size)
            req = urllib.request.Request(
                url,
                data=progress_file,
                headers={
                    "Authorization": f"Basic {auth_string}",
                    "Content-Type": "application/octet-stream",
                    "Content-Length": str(file_size),
                },
                method="POST",
            )
            print("  🚀 Uploading...", flush=True)
            with urllib.request.urlopen(req, context=ssl_context, timeout=3600) as response:
                response_data = response.read().decode()
                result = json.loads(response_data)

                print("")
                print("")
                if result.get("ok"):
                    print("✅ Upload successful!")
                    print("")
                    print("Next steps:")
                    print("  1. Verify: ./pikvm.sh --iso --list")
                    print("  2. Boot: ./pikvm")
                    return True
                print(f"❌ Upload failed: {result}")
                return False

    except urllib.error.HTTPError as e:
        print("")
        print(f"❌ HTTP Error {e.code}: {e.reason}")
        try:
            error_body = e.read().decode()
            print(f"   Details: {error_body}")
        except Exception:
            pass
        return False
    except urllib.error.URLError as e:
        print("")
        print(f"❌ Connection error: {e.reason}")
        return False
    except Exception as e:
        print("")
        print(f"❌ Error during upload: {e}")
        import traceback

        traceback.print_exc()
        return False


# --- Upload: requests ---


def upload_iso_requests(iso_path: str, config: dict[str, str]) -> bool:
    requests = _requests()

    if not os.path.exists(iso_path):
        print(f"❌ Error: File not found: {iso_path}")
        return False

    filename = os.path.basename(iso_path)
    file_size = os.path.getsize(iso_path)
    file_size_mb = file_size // (1024 * 1024)

    host = config["PIKVM_HOST"]
    user = config["PIKVM_USER"]
    password = config["PIKVM_PASS"]

    url = f"https://{host}/api/msd/write?image={filename}"

    print(f"📀 Uploading {filename}")
    print(f"   Size: {file_size_mb} MB")
    print(f"   Host: {host}")
    print("")
    print("⏳ Starting streaming upload (requests)...")
    print("")

    try:
        with open(iso_path, "rb") as f:

            def file_gen(file_obj, chunk_size: int = 8192):
                total_read = 0
                while True:
                    chunk = file_obj.read(chunk_size)
                    if not chunk:
                        break
                    total_read += len(chunk)
                    if total_read % (50 * 1024 * 1024) < chunk_size:
                        progress_mb = total_read // (1024 * 1024)
                        percent = (total_read / file_size) * 100
                        print(f"  📊 Progress: {progress_mb} MB / {file_size_mb} MB ({percent:.1f}%)")
                    yield chunk

            response = requests.post(
                url,
                data=file_gen(f),
                auth=(user, password),
                verify=False,
                headers={"Content-Type": "application/octet-stream"},
                timeout=3600,
            )

        print("")
        if response.status_code == 200:
            result = response.json()
            if result.get("ok"):
                print("✅ Upload successful!")
                print("")
                print("Next steps:")
                print("  1. Verify: ./pikvm.sh --iso --list")
                print("  2. Boot: ./pikvm")
                return True
            print(f"❌ Upload failed: {result}")
            return False
        print(f"❌ HTTP Error {response.status_code}: {response.text}")
        return False

    except requests.exceptions.Timeout:  # type: ignore[attr-defined]
        print("❌ Upload timed out (file too large or connection too slow)")
        return False
    except Exception as e:
        print(f"❌ Error during upload: {e}")
        return False


def run_upload_builtin(iso_path: str) -> int:
    config = load_env()
    return 0 if upload_iso_builtin(iso_path, config) else 1


def run_upload_large(iso_path: str) -> int:
    config = load_env()
    return 0 if upload_iso_requests(iso_path, config) else 1


def _verbose_from_env() -> bool:
    return os.environ.get("PIKVM_VERBOSE", "").strip().lower() in ("1", "true", "yes", "on")


def switch_print_summary_from_payload(data: dict | None, *, prefix: str = "") -> None:
    """Print result.summary.active_port / active_id from GET /api/switch (KVMD source of truth)."""
    if not data:
        print(f"{prefix}[switch] (empty state)")
        return
    root = data.get("result", data)
    if not isinstance(root, dict):
        print(f"{prefix}[switch] unexpected state shape")
        return
    summ = root.get("summary")
    if not isinstance(summ, dict):
        print(f"{prefix}[switch] no result.summary (older firmware?); see full JSON dump.")
        return
    ap = summ.get("active_port")
    aid = summ.get("active_id")
    synced = summ.get("synced")
    print(
        f"{prefix}[switch] summary: active_port={ap!r} active_id={aid!r} synced={synced!r} "
        f"(active_id is what the PiKVM UI usually shows as the port number)"
    )
    if isinstance(ap, int) and ap >= 0 and aid is not None:
        print(
            f"{prefix}         Mapping: set_active?port={ap} selects UI id {aid!r}. "
            f"For UI \"Port 2\" use API port 1; for UI \"Port 3\" use API port 2."
        )


def switch_dump_state(config: dict[str, str], *, prefix: str = "", max_chars: int = 3500) -> None:
    """Pretty-print GET /api/switch (truncated). Fails silently except prints one line."""
    st = switch_get_state(config)
    if st is None:
        print(f"{prefix}[switch] GET /api/switch failed (timeout or parse error)")
        return
    switch_print_summary_from_payload(st, prefix=prefix)
    root = st.get("result", st)
    text = json.dumps(root, indent=2)
    if len(text) > max_chars:
        text = text[:max_chars] + "\n… (truncated)"
    print(f"{prefix}[switch] GET /api/switch:\n{text}")


def switch_set_active(
    config: dict[str, str],
    port: int,
    *,
    verbose: bool = False,
) -> bool:
    """POST /api/switch/set_active so HDMI/snapshot follow this extender port (0-based KVMD index)."""
    host = config["PIKVM_HOST"]
    user = config["PIKVM_USER"]
    password = config["PIKVM_PASS"]
    url = f"https://{host}/api/switch/set_active?port={port}"
    req = urllib.request.Request(url, method="POST")
    creds = b64encode(f"{user}:{password}".encode()).decode()
    req.add_header("Authorization", f"Basic {creds}")
    ctx = ssl._create_unverified_context()
    if verbose:
        print(
            f"[switch] POST set_active?port={port}  "
            f"(KVMD uses 0-based index; PiKVM web UI often shows Port {port + 1})"
        )
        print(f"         URL: {url}")
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=15) as r:
            body = r.read().decode(errors="replace").strip()
            ok = 200 <= r.status < 300
            if verbose:
                print(f"         HTTP {r.status} body: {body[:500] or '(empty)'}")
                st = switch_get_state(config)
                switch_print_summary_from_payload(st, prefix="         ")
            return ok
    except urllib.error.HTTPError as e:
        err_body = ""
        try:
            err_body = e.read().decode(errors="replace")[:400]
        except Exception:
            pass
        print(f"❌ switch/set_active HTTP {e.code}: {e.reason} {err_body}")
        return False
    except Exception as e:
        print(f"❌ switch/set_active failed: {e}")
        return False


# --- PiKVM HID (same idea as pikvm.go sendText / sendKey: types on the *remote* host) ---

_PIKVM_KEY_ALIASES: dict[str, str] = {
    "enter": "Enter",
    "return": "Enter",
    "tab": "Tab",
    "esc": "Escape",
    "escape": "Escape",
    "backspace": "Backspace",
    "delete": "Delete",
    "del": "Delete",
    "space": "Space",
    "up": "ArrowUp",
    "down": "ArrowDown",
    "left": "ArrowLeft",
    "right": "ArrowRight",
    "pageup": "PageUp",
    "pagedown": "PageDown",
    "home": "Home",
    "end": "End",
    "insert": "Insert",
    "win": "MetaLeft",
    "command": "MetaLeft",
    "ctrl": "ControlLeft",
    "shift": "ShiftLeft",
    "alt": "AltLeft",
}


def _pikvm_key_web_name(k: str) -> str:
    """Map PyAutoGUI-style names to KVMD keymap web_name (see kvmd keymap.csv)."""
    s = str(k).strip()
    low = s.lower()
    if low in _PIKVM_KEY_ALIASES:
        return _PIKVM_KEY_ALIASES[low]
    if len(s) == 1 and s.isalpha():
        return "Key" + s.upper()
    if len(s) == 1 and s.isdigit():
        return "Digit" + s
    if low.startswith("f") and len(low) > 1 and low[1:].isdigit():
        return "F" + low[1:]
    return s


def _pikvm_auth_request(
    config: dict[str, str],
    url: str,
    *,
    method: str = "GET",
    data: bytes | None = None,
    content_type: str | None = None,
    timeout: float = 60.0,
) -> tuple[int, str]:
    user = config["PIKVM_USER"]
    password = config["PIKVM_PASS"]
    req = urllib.request.Request(url, data=data, method=method)
    creds = b64encode(f"{user}:{password}".encode()).decode()
    req.add_header("Authorization", f"Basic {creds}")
    if content_type:
        req.add_header("Content-Type", content_type)
    ctx = ssl._create_unverified_context()
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=timeout) as r:
            body = r.read().decode(errors="replace")
            return r.status, body
    except urllib.error.HTTPError as e:
        try:
            err = e.read().decode(errors="replace")[:500]
        except Exception:
            err = ""
        return e.code, err or e.reason


def hid_print(
    config: dict[str, str],
    text: str,
    *,
    type_interval: float = 0.0,
    verbose: bool = False,
) -> bool:
    """POST /api/hid/print — type text on the machine attached to PiKVM (active switch port)."""
    host = config["PIKVM_HOST"]
    params: list[tuple[str, str]] = []
    if type_interval > 0:
        params.append(("slow", "yes"))
        params.append(("delay", f"{min(type_interval, 5.0):.4f}"))
    q = ("?" + urllib.parse.urlencode(params)) if params else ""
    url = f"https://{host}/api/hid/print{q}"
    body = text.encode("utf-8")
    if verbose:
        print(f"[hid] POST /api/hid/print ({len(body)} bytes text, slow={bool(params)})")
    status, resp = _pikvm_auth_request(
        config,
        url,
        method="POST",
        data=body,
        content_type="text/plain; charset=utf-8",
        timeout=120.0,
    )
    if status < 200 or status >= 300:
        print(f"❌ hid/print HTTP {status}: {resp[:400]}")
        return False
    try:
        j = json.loads(resp) if resp.strip() else {}
        if j.get("ok") is False:
            print(f"❌ hid/print: {resp[:400]}")
            return False
    except json.JSONDecodeError:
        pass
    return True


def hid_send_key(config: dict[str, str], key: str, *, verbose: bool = False) -> bool:
    """POST /api/hid/events/send_key — one key on the remote host (web_name from keymap)."""
    host = config["PIKVM_HOST"]
    web = _pikvm_key_web_name(key)
    qs = urllib.parse.urlencode({"key": web, "finish": "yes"})
    url = f"https://{host}/api/hid/events/send_key?{qs}"
    if verbose:
        print(f"[hid] POST send_key key={web!r}")
    status, resp = _pikvm_auth_request(config, url, method="POST", data=b"", timeout=30.0)
    if status < 200 or status >= 300:
        print(f"❌ hid/events/send_key HTTP {status} (key={web!r}): {resp[:400]}")
        return False
    try:
        j = json.loads(resp) if resp.strip() else {}
        if j.get("ok") is False:
            print(f"❌ hid/events/send_key: {resp[:400]}")
            return False
    except json.JSONDecodeError:
        pass
    return True


def stream_keeper_try_start(config: dict[str, str]) -> subprocess.Popen | None:
    """Background websocat on /api/ws?stream=1 so the video streamer stays up for snapshots."""
    wsoc = shutil.which("websocat")
    if not wsoc:
        return None
    host = config["PIKVM_HOST"]
    user = config["PIKVM_USER"]
    password = config["PIKVM_PASS"]
    keeper_url = f"wss://{host}/api/ws?stream=1"
    return subprocess.Popen(
        [
            wsoc,
            "-k",
            keeper_url,
            "-H",
            f"X-KVMD-User: {user}",
            "-H",
            f"X-KVMD-Passwd: {password}",
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


def switch_get_state(config: dict[str, str]) -> dict | None:
    """GET /api/switch (for debugging). Shape varies by firmware."""
    host = config["PIKVM_HOST"]
    user = config["PIKVM_USER"]
    password = config["PIKVM_PASS"]
    url = f"https://{host}/api/switch"
    req = urllib.request.Request(url)
    creds = b64encode(f"{user}:{password}".encode()).decode()
    req.add_header("Authorization", f"Basic {creds}")
    ctx = ssl._create_unverified_context()
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=10) as r:
            return json.loads(r.read().decode())
    except Exception:
        return None


def fetch_snapshot_jpeg(config: dict[str, str], quality: int) -> bytes | None:
    """GET /api/streamer/snapshot — returns JPEG bytes, or None if streamer not ready (503)."""
    host = config["PIKVM_HOST"]
    user = config["PIKVM_USER"]
    password = config["PIKVM_PASS"]
    qs = urllib.parse.urlencode({"save": "1", "preview_quality": str(quality)})
    url = f"https://{host}/api/streamer/snapshot?{qs}"
    req = urllib.request.Request(url)
    creds = b64encode(f"{user}:{password}".encode()).decode()
    req.add_header("Authorization", f"Basic {creds}")
    ctx = ssl._create_unverified_context()
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=60) as r:
            return r.read()
    except urllib.error.HTTPError as e:
        if e.code == 503:
            return None
        body = ""
        try:
            body = e.read().decode(errors="replace")[:200]
        except Exception:
            pass
        print(f"❌ snapshot HTTP {e.code}: {e.reason} {body}")
        return None
    except Exception as e:
        print(f"❌ snapshot failed: {e}")
        return None


def run_capture(args: argparse.Namespace) -> int:
    """
    Periodically save PiKVM streamer frames to disk for a given switch port.
    Keeps the streamer alive with websocat (same idea as pikvm.sh --stream) unless --no-keeper.
    """
    config = load_env()
    script_dir = Path(__file__).resolve().parent
    out = Path(args.output_dir)
    if not out.is_absolute():
        out = (script_dir / out).resolve()
    out.mkdir(parents=True, exist_ok=True)

    port = int(args.port)
    interval = float(args.interval)
    keeper: subprocess.Popen | None = None

    if not args.no_keeper:
        wsoc = shutil.which("websocat")
        if not wsoc:
            print(
                "❌ websocat not in PATH. Install it (e.g. brew install websocat), "
                "or run with --no-keeper while something else keeps the PiKVM stream open (browser/TUI)."
            )
            return 1
        host = config["PIKVM_HOST"]
        user = config["PIKVM_USER"]
        password = config["PIKVM_PASS"]
        keeper_url = f"wss://{host}/api/ws?stream=1"
        keeper = subprocess.Popen(
            [
                wsoc,
                "-k",
                keeper_url,
                "-H",
                f"X-KVMD-User: {user}",
                "-H",
                f"X-KVMD-Passwd: {password}",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        print("Stream keeper started (websocat → /api/ws?stream=1)")
    else:
        print("(no keeper — streamer must already be active, e.g. browser or another client)")

    try:
        time.sleep(float(args.warmup))
        reassert = not bool(getattr(args, "no_reassert_port", False))
        settle = float(args.settle)
        verbose = bool(getattr(args, "verbose", False))

        if not reassert:
            if not switch_set_active(config, port, verbose=verbose):
                return 1
            time.sleep(settle)
            if verbose:
                st = switch_get_state(config)
                if st is not None:
                    print(f"  [verbose] /api/switch: {json.dumps(st)[:240]}…")

        print(f"Capturing port {port} (0-based KVMD index) every {interval}s → {out}")
        if reassert:
            print("  Re-selecting switch port before each snapshot (use --no-reassert-port to disable).")
        print("  Ctrl+C to stop.")
        seq = 0
        deadline = time.time() + float(args.duration) if args.duration is not None else None

        while True:
            if deadline is not None and time.time() >= deadline:
                break
            if reassert:
                if not switch_set_active(config, port, verbose=verbose):
                    print("  ⚠️ switch/set_active failed; retry in 1s")
                    time.sleep(1)
                    continue
                time.sleep(settle)
                if verbose and seq == 0:
                    st = switch_get_state(config)
                    if st is not None:
                        print(f"  [verbose] /api/switch: {json.dumps(st)[:240]}…")

            data = fetch_snapshot_jpeg(config, int(args.quality))
            if data is None:
                print("  … snapshot unavailable (503?) — retry in 1s (stream still starting?)")
                time.sleep(1)
                continue
            seq += 1
            stamp = datetime.now().strftime("%Y%m%d_%H%M%S_%f")[:-3]
            fp = out / f"port{port}_{stamp}_{seq:06d}.jpg"
            fp.write_bytes(data)
            print(f"  saved {fp.name} ({len(data) // 1024} KB)")
            time.sleep(max(0.05, interval))
    except KeyboardInterrupt:
        print("\n👋 Capture stopped.")
        return 0
    finally:
        if keeper is not None and keeper.poll() is None:
            keeper.terminate()
            try:
                keeper.wait(timeout=3)
            except subprocess.TimeoutExpired:
                keeper.kill()

    return 0


def pikvm_template_match_score(screen_bgr, templ_bgr) -> float | None:
    """OpenCV matchTemplate max correlation (0..1). None if template larger than screen."""
    import cv2

    if screen_bgr is None or templ_bgr is None or templ_bgr.size == 0:
        return None
    sh, sw = screen_bgr.shape[:2]
    th, tw = templ_bgr.shape[:2]
    if th > sh or tw > sw or th < 1 or tw < 1:
        return None
    if len(screen_bgr.shape) == 2:
        sg = screen_bgr
    else:
        sg = cv2.cvtColor(screen_bgr, cv2.COLOR_BGR2GRAY)
    if len(templ_bgr.shape) == 2:
        tg = templ_bgr
    else:
        tg = cv2.cvtColor(templ_bgr, cv2.COLOR_BGR2GRAY)
    res = cv2.matchTemplate(sg, tg, cv2.TM_CCOEFF_NORMED)
    _mn, max_v, _ml, _mx = cv2.minMaxLoc(res)
    return float(max_v)


def wait_for_pikvm_template(
    config: dict[str, str],
    template_path: Path,
    *,
    port: int,
    timeout: float,
    interval: float,
    settle: float,
    threshold: float,
    quality: int,
    verbose: bool = False,
) -> bool:
    """Poll PiKVM HTTP snapshots (given switch port) until template image matches."""
    try:
        import cv2
        import numpy as np
    except ImportError:
        print("❌ wait_image_pikvm requires opencv-python (and numpy). pip install opencv-python")
        return False

    templ = cv2.imread(str(template_path), cv2.IMREAD_COLOR)
    if templ is None:
        print(f"❌ Could not read template image: {template_path}")
        return False

    deadline = time.time() + timeout
    print(
        f"… PiKVM API snapshot match (port {port}, threshold≥{threshold:.2f}, timeout {timeout}s): "
        f"{template_path.name}"
    )
    if verbose:
        print(
            "  Verbose: if the PiKVM UI shows a different port number, note KVMD uses 0-based API "
            f"indices (API port {port} → UI often labels it Port {port + 1})."
        )

    attempt = 0
    while time.time() < deadline:
        attempt += 1
        if not switch_set_active(config, port, verbose=verbose):
            time.sleep(1)
            continue
        time.sleep(settle)
        if verbose and attempt == 1:
            switch_dump_state(config, prefix="         ")

        jpeg = fetch_snapshot_jpeg(config, quality)
        if jpeg is None:
            if verbose:
                print(f"         poll #{attempt}: snapshot 503/unavailable")
            time.sleep(max(interval, 0.5))
            continue
        screen = cv2.imdecode(np.frombuffer(jpeg, np.uint8), cv2.IMREAD_COLOR)
        if screen is None:
            if verbose:
                print(f"         poll #{attempt}: imdecode failed ({len(jpeg)} bytes)")
            time.sleep(interval)
            continue
        score = pikvm_template_match_score(screen, templ)
        if score is None:
            if verbose:
                sh, sw = screen.shape[:2]
                th, tw = templ.shape[:2]
                print(
                    f"         poll #{attempt}: no score (screen {sw}x{sh}, template {tw}x{th} "
                    f"— template must fit inside snapshot)"
                )
            time.sleep(interval)
            continue
        if verbose:
            print(
                f"         poll #{attempt}: template score={score:.3f} (need ≥{threshold:.2f}) "
                f"jpeg={len(jpeg) // 1024}KB screen={screen.shape[1]}x{screen.shape[0]}"
            )
        if verbose and attempt > 1 and attempt % 12 == 0:
            switch_dump_state(config, prefix="         ", max_chars=2000)

        if score >= threshold:
            print(f"✓ Match on PiKVM feed: score={score:.3f}")
            if verbose:
                switch_dump_state(config, prefix="         ", max_chars=2000)
            return True
        time.sleep(interval)

    print(f"❌ Timeout waiting for PiKVM template: {template_path}")
    return False


def _import_pyautogui():
    try:
        import pyautogui

        return pyautogui
    except ImportError:
        return None


def pyautogui_wait_image(
    pyautogui_module,
    image_path: Path,
    *,
    timeout: float,
    interval: float,
    confidence: float | None,
    click: bool,
    after_match: float,
) -> bool:
    """Poll until template is on screen; optionally click and sleep. Returns False on timeout or locate error."""
    import traceback

    locate_kwargs: dict = {}
    if confidence is not None:
        locate_kwargs["confidence"] = confidence

    deadline = time.time() + timeout
    print(f"… waiting for LOCAL screen template (timeout {timeout}s): {image_path}")
    print("  PyAutoGUI sees your Mac display only — PiKVM must be visible in a window here.")
    while time.time() < deadline:
        try:
            if locate_kwargs:
                pos = pyautogui_module.locateCenterOnScreen(str(image_path), **locate_kwargs)
            else:
                pos = pyautogui_module.locateCenterOnScreen(str(image_path))
        except Exception as e:
            msg = str(e).strip()
            print(f"❌ locateCenterOnScreen failed: {type(e).__name__}: {msg or '(no message)'}")
            if not msg:
                print(
                    "   macOS: System Settings → Privacy & Security → Screen Recording → enable for "
                    "Terminal (or Cursor/iTerm). Then quit and reopen the app."
                )
            if confidence is not None:
                print("   Or install opencv-python / adjust --confidence on the step.")
            if os.environ.get("PIKVM_DEBUG_GUI"):
                traceback.print_exc()
            return False
        if pos is not None:
            print(f"✓ Matched at {pos}")
            if click:
                pyautogui_module.click(pos)
            if after_match > 0:
                time.sleep(after_match)
            return True
        time.sleep(interval)

    print(f"❌ Timeout waiting for image: {image_path}")
    return False


def run_automate(args: argparse.Namespace) -> int:
    """Wait until a template image appears on the local screen, then type text (focused window)."""
    pyautogui = _import_pyautogui()
    if pyautogui is None:
        print("❌ PyAutoGUI not installed. Run: pip install pyautogui Pillow")
        print("   For --confidence: pip install opencv-python")
        return 1

    script_dir = Path(__file__).resolve().parent
    image_arg = Path(args.image)
    if image_arg.is_absolute():
        image_path = image_arg
    else:
        image_path = (script_dir / args.image_dir / args.image).resolve()
    if not image_path.is_file():
        print(f"❌ Image not found: {image_path}")
        print(f"   Put PNG templates under {script_dir / args.image_dir}/")
        return 1

    pyautogui.FAILSAFE = True
    pyautogui.PAUSE = args.pause

    print(f"Waiting for template on screen: {image_path}")
    print("  (Move mouse to the screen corner to abort — PyAutoGUI failsafe)")

    if not pyautogui_wait_image(
        pyautogui,
        image_path,
        timeout=float(args.timeout),
        interval=float(args.interval),
        confidence=args.confidence,
        click=bool(args.click),
        after_match=float(args.after_match),
    ):
        return 1

    pyautogui.write(args.text, interval=args.type_interval)
    print(f"✓ Typed: {args.text!r}")
    return 0


def _load_sequence_document(path: Path) -> tuple[dict, list[dict]]:
    raw = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(raw, list):
        return {}, raw
    if isinstance(raw, dict) and "steps" in raw:
        steps = raw["steps"]
        if not isinstance(steps, list):
            raise ValueError("'steps' must be a JSON array")
        meta = {k: v for k, v in raw.items() if k not in ("steps", "description")}
        return meta, steps
    raise ValueError("sequence must be a JSON array or an object with a 'steps' array")


def _resolve_sequence_path(repo_root: Path, p: str) -> Path:
    path = Path(p)
    if path.is_absolute():
        return path.resolve()
    return (repo_root / path).resolve()


def run_sequence(args: argparse.Namespace) -> int:
    """
    Run automation steps from a JSON file (wait_image, wait_image_pikvm, type/write, key/press, sleep).
    Paths in steps are relative to the directory containing this file (automation/).
    wait_image_pikvm matches against PiKVM HTTP snapshots for a switch port (not the Mac desktop).
    If the sequence includes wait_image_pikvm, type/key default to PiKVM HID (/api/hid/print, like Go sendText).
    """
    script_path = Path(args.script).resolve()
    if not script_path.is_file():
        print(f"❌ Sequence file not found: {script_path}")
        return 1

    repo_root = Path(__file__).resolve().parent
    try:
        meta, steps = _load_sequence_document(script_path)
    except (json.JSONDecodeError, ValueError) as e:
        print(f"❌ Invalid sequence file: {e}")
        return 1

    needs_pikvm_snap = any(isinstance(s, dict) and "wait_image_pikvm" in s for s in steps)
    has_type_or_key = any(
        isinstance(s, dict) and (set(s) & {"type", "write", "key", "press"}) for s in steps
    )
    has_wait_image = any(isinstance(s, dict) and "wait_image" in s for s in steps)

    raw_inp = meta.get("input")
    if isinstance(raw_inp, str):
        input_mode = raw_inp.strip().lower()
    else:
        input_mode = str(getattr(args, "sequence_input", "auto")).strip().lower()
    if input_mode not in ("auto", "pikvm", "local"):
        print(f"❌ Invalid input mode {input_mode!r} (use auto, pikvm, or local)")
        return 1
    if input_mode == "auto":
        use_pikvm_hid = needs_pikvm_snap
    elif input_mode == "pikvm":
        use_pikvm_hid = True
    else:
        use_pikvm_hid = False

    needs_pyautogui = has_wait_image or (has_type_or_key and not use_pikvm_hid)

    pyautogui = None
    if needs_pyautogui:
        pyautogui = _import_pyautogui()
        if pyautogui is None:
            print("❌ PyAutoGUI not installed. Run: pip install pyautogui Pillow")
            return 1
        pyautogui.FAILSAFE = True
        pyautogui.PAUSE = float(args.pause)

    default_type_interval = float(args.type_interval)
    default_port = 2
    if isinstance(meta.get("defaults"), dict) and "port" in meta["defaults"]:
        default_port = int(meta["defaults"]["port"])
    if "pikvm_port" in meta:
        default_port = int(meta["pikvm_port"])
    if getattr(args, "pikvm_port", None) is not None:
        default_port = int(args.pikvm_port)

    snap_quality = int(getattr(args, "snapshot_quality", 95))
    warmup = float(meta.get("warmup", getattr(args, "warmup", 5.0)))
    seq_verbose = (
        bool(meta.get("verbose", False))
        or bool(getattr(args, "verbose", False))
        or _verbose_from_env()
    )

    config: dict[str, str] | None = None
    keeper: subprocess.Popen | None = None

    print(f"Running sequence ({len(steps)} steps): {script_path}")
    if needs_pikvm_snap:
        print(f"  PiKVM snapshot steps use switch port {default_port} (override with JSON pikvm_port or --pikvm-port).")
        if seq_verbose:
            print("  Verbose switch logging on (--verbose or PIKVM_VERBOSE=1 or JSON \"verbose\": true).")
    if use_pikvm_hid and has_type_or_key:
        print("  Typing/key presses use PiKVM HID (POST /api/hid/print and send_key) — same as Go testKeyboard.")
    elif has_type_or_key:
        print("  Typing uses PyAutoGUI on this Mac — focus the target window. Failsafe: move mouse to screen corner.")

    try:
        if needs_pikvm_snap:
            config = load_env()
            use_keeper = bool(meta.get("stream_keeper", True)) and not bool(
                getattr(args, "no_stream_keeper", False)
            )
            if use_keeper:
                keeper = stream_keeper_try_start(config)
                if keeper is None:
                    print(
                        "⚠️  websocat not in PATH — install (brew install websocat) or set "
                        "\"stream_keeper\": false in JSON if the stream is already active."
                    )
                else:
                    print("  Stream keeper started for PiKVM snapshots.")
            time.sleep(warmup)
        elif use_pikvm_hid:
            config = load_env()

        for i, step in enumerate(steps, start=1):
            if not isinstance(step, dict):
                print(f"❌ Step {i}: expected JSON object, got {type(step).__name__}")
                return 1

            if "wait_image_pikvm" in step:
                if config is None:
                    config = load_env()
                rel = step["wait_image_pikvm"]
                if not isinstance(rel, str):
                    print(f"❌ Step {i}: wait_image_pikvm must be a string path")
                    return 1
                img_path = _resolve_sequence_path(repo_root, rel)
                if not img_path.is_file():
                    print(f"❌ Step {i}: template not found: {img_path}")
                    return 1
                port = int(step.get("port", default_port))
                timeout = float(step.get("timeout", 600))
                poll = float(step.get("interval", 1.0))
                settle = float(step.get("settle", 0.35))
                threshold = float(step.get("threshold", 0.82))
                qual = int(step.get("quality", snap_quality))
                step_verbose = bool(step.get("verbose", False)) or seq_verbose
                if not wait_for_pikvm_template(
                    config,
                    img_path,
                    port=port,
                    timeout=timeout,
                    interval=poll,
                    settle=settle,
                    threshold=threshold,
                    quality=qual,
                    verbose=step_verbose,
                ):
                    return 1
                continue

            if "wait_image" in step:
                if pyautogui is None:
                    pyautogui = _import_pyautogui()
                    if pyautogui is None:
                        print("❌ PyAutoGUI required for wait_image")
                        return 1
                    pyautogui.FAILSAFE = True
                    pyautogui.PAUSE = float(args.pause)
                rel = step["wait_image"]
                if not isinstance(rel, str):
                    print(f"❌ Step {i}: wait_image must be a string path")
                    return 1
                img_path = _resolve_sequence_path(repo_root, rel)
                if not img_path.is_file():
                    print(f"❌ Step {i}: image not found: {img_path}")
                    return 1
                timeout = float(step.get("timeout", 600))
                poll = float(step.get("interval", 0.5))
                conf = step.get("confidence", None)
                if conf is not None:
                    conf = float(conf)
                click = bool(step.get("click", False))
                after = float(step.get("after_match", 0))
                if not pyautogui_wait_image(
                    pyautogui,
                    img_path,
                    timeout=timeout,
                    interval=poll,
                    confidence=conf,
                    click=click,
                    after_match=after,
                ):
                    return 1
                continue

            if "type" in step or "write" in step:
                text = step.get("type")
                if text is None:
                    text = step.get("write")
                if text is None:
                    print(f"❌ Step {i}: empty type/write")
                    return 1
                ti = float(step.get("type_interval", default_type_interval))
                if use_pikvm_hid:
                    if config is None:
                        config = load_env()
                    if not hid_print(
                        config,
                        str(text),
                        type_interval=ti,
                        verbose=seq_verbose,
                    ):
                        return 1
                    print(f"✓ Step {i}: typed {str(text)!r} (PiKVM HID)")
                else:
                    if pyautogui is None:
                        pyautogui = _import_pyautogui()
                        if pyautogui is None:
                            return 1
                        pyautogui.FAILSAFE = True
                        pyautogui.PAUSE = float(args.pause)
                    pyautogui.write(str(text), interval=ti)
                    print(f"✓ Step {i}: typed {str(text)!r}")
                continue

            if "key" in step or "press" in step:
                k = step.get("key")
                if k is None:
                    k = step.get("press")
                if not k:
                    print(f"❌ Step {i}: missing key name")
                    return 1
                if use_pikvm_hid:
                    if config is None:
                        config = load_env()
                    if not hid_send_key(config, str(k), verbose=seq_verbose):
                        return 1
                    print(f"✓ Step {i}: press {str(k)!r} (PiKVM HID → {_pikvm_key_web_name(str(k))!r})")
                else:
                    if pyautogui is None:
                        pyautogui = _import_pyautogui()
                        if pyautogui is None:
                            return 1
                        pyautogui.FAILSAFE = True
                        pyautogui.PAUSE = float(args.pause)
                    pyautogui.press(str(k))
                    print(f"✓ Step {i}: press {str(k)!r}")
                continue

            if "sleep" in step:
                time.sleep(float(step["sleep"]))
                print(f"✓ Step {i}: sleep {step['sleep']}s")
                continue

            print(
                f"❌ Step {i}: unknown keys {sorted(step.keys())!r} "
                "(expected wait_image, wait_image_pikvm, type/write, key/press, sleep)"
            )
            return 1
    except KeyboardInterrupt:
        print("\n👋 Sequence interrupted.")
        return 0
    finally:
        if keeper is not None and keeper.poll() is None:
            keeper.terminate()
            try:
                keeper.wait(timeout=3)
            except subprocess.TimeoutExpired:
                keeper.kill()

    print("✓ Sequence finished.")
    return 0


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="pikvm",
        description="PiKVM: ATX control, MSD ISO upload, and local GUI automation.",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    p_control = sub.add_parser(
        "control",
        help="Interactive ATX power / reset menu",
    )
    p_control.set_defaults(_handler="control")

    p_builtin = sub.add_parser(
        "upload-builtin",
        aliases=("upload-iso-builtin",),
        help="Upload ISO using only stdlib (urllib); good when requests is not wanted",
    )
    p_builtin.add_argument("iso", help="Path to .iso file")
    p_builtin.set_defaults(_handler="upload-builtin")

    p_large = sub.add_parser(
        "upload-large",
        aliases=("upload-iso-large",),
        help="Upload ISO using requests streaming",
    )
    p_large.add_argument("iso", help="Path to .iso file")
    p_large.set_defaults(_handler="upload-large")

    p_auto = sub.add_parser(
        "automate",
        help="Wait for a template image on the local screen, then type text (PyAutoGUI)",
    )
    p_auto.add_argument(
        "--image",
        required=True,
        metavar="FILE",
        help="Template image: filename under --image-dir, or an absolute path to a PNG",
    )
    p_auto.add_argument(
        "--image-dir",
        default="images",
        help="Directory for templates (default: ./images next to pikvm.py)",
    )
    p_auto.add_argument(
        "--text",
        default="hello world",
        help='Text to type after the image appears (default: "hello world")',
    )
    p_auto.add_argument("--timeout", type=float, default=300.0, help="Seconds to wait (default: 300)")
    p_auto.add_argument("--interval", type=float, default=0.5, help="Poll interval in seconds")
    p_auto.add_argument(
        "--confidence",
        type=float,
        default=None,
        metavar="0.0-1.0",
        help="Optional match strictness; requires: pip install opencv-python",
    )
    p_auto.add_argument(
        "--no-click",
        action="store_true",
        help="Do not click the center of the match before typing",
    )
    p_auto.add_argument("--pause", type=float, default=0.05, help="PyAutoGUI pause between actions")
    p_auto.add_argument("--type-interval", type=float, default=0.03, help="Delay between keystrokes")
    p_auto.add_argument(
        "--after-match",
        type=float,
        default=0.2,
        help="Seconds to wait after match (and optional click) before typing",
    )
    p_auto.set_defaults(_handler="automate")

    p_cap = sub.add_parser(
        "capture",
        aliases=("capture-screenshots", "screenshots"),
        help="Save PiKVM /api/streamer/snapshot JPEGs on an interval (default port 2 → ./images)",
    )
    p_cap.add_argument(
        "--port",
        type=int,
        default=2,
        help="KVMD switch / extender port index, 0-based (default: 2)",
    )
    p_cap.add_argument(
        "--interval",
        type=float,
        default=1.0,
        help="Seconds between snapshots (default: 1)",
    )
    p_cap.add_argument(
        "--output-dir",
        default="images",
        metavar="DIR",
        help="Directory for JPEG files (default: ./images next to pikvm.py; absolute paths OK)",
    )
    p_cap.add_argument(
        "--duration",
        type=float,
        default=None,
        metavar="SEC",
        help="Stop after this many seconds (default: run until Ctrl+C)",
    )
    p_cap.add_argument(
        "--quality",
        type=int,
        default=95,
        help="JPEG quality for PiKVM snapshot API (default: 95)",
    )
    p_cap.add_argument(
        "--no-keeper",
        action="store_true",
        help="Do not spawn websocat; use when the streamer is already active elsewhere",
    )
    p_cap.add_argument(
        "--warmup",
        type=float,
        default=5.0,
        metavar="SEC",
        help="Sleep after keeper starts (or always before first snapshot); default 5",
    )
    p_cap.add_argument(
        "--settle",
        type=float,
        default=0.35,
        metavar="SEC",
        help="Sleep after each switch/set_active before snapshot; default 0.35",
    )
    p_cap.add_argument(
        "--no-reassert-port",
        action="store_true",
        help="Call switch/set_active only once at start (default: before every snapshot for stable port)",
    )
    p_cap.add_argument(
        "--verbose",
        "-v",
        action="store_true",
        help="Print truncated GET /api/switch JSON after first successful set_active",
    )
    p_cap.set_defaults(_handler="capture")

    p_runseq = sub.add_parser(
        "run-sequence",
        aliases=("sequence", "seq"),
        help="Run JSON automation: wait_image, type/write, key/press (see seq-script/)",
    )
    p_runseq.add_argument(
        "script",
        help="Path to sequence JSON (e.g. automation/seq-script/port2-alarm.json)",
    )
    p_runseq.add_argument(
        "--input",
        dest="sequence_input",
        choices=("auto", "pikvm", "local"),
        default="auto",
        help="Where type/key go: auto=PiKVM HID if any wait_image_pikvm else PyAutoGUI; pikvm|local=force",
    )
    p_runseq.add_argument("--pause", type=float, default=0.05, help="PyAutoGUI pause between actions")
    p_runseq.add_argument(
        "--type-interval",
        type=float,
        default=0.03,
        help="Default delay between keystrokes for type/write steps",
    )
    p_runseq.add_argument(
        "--pikvm-port",
        type=int,
        default=None,
        help="Default KVMD switch port for wait_image_pikvm steps (overrides JSON pikvm_port)",
    )
    p_runseq.add_argument(
        "--no-stream-keeper",
        action="store_true",
        help="Do not spawn websocat; PiKVM stream must already be active",
    )
    p_runseq.add_argument(
        "--warmup",
        type=float,
        default=5.0,
        help="Seconds to wait after starting stream keeper before first PiKVM snapshot step",
    )
    p_runseq.add_argument(
        "--snapshot-quality",
        type=int,
        default=95,
        help="JPEG quality for PiKVM snapshot API during wait_image_pikvm",
    )
    p_runseq.add_argument(
        "--verbose",
        "-v",
        action="store_true",
        help="Log each set_active, template scores, and periodic GET /api/switch (or set PIKVM_VERBOSE=1)",
    )
    p_runseq.set_defaults(_handler="run-sequence")

    return parser


def main(argv: list[str] | None = None) -> int:
    argv = argv if argv is not None else sys.argv[1:]
    parser = _build_parser()
    args = parser.parse_args(argv)
    if getattr(args, "_handler", None) == "automate":
        args.click = not getattr(args, "no_click", False)

    handler = getattr(args, "_handler", None)
    if handler == "control":
        try:
            return run_control()
        except KeyboardInterrupt:
            print("\n\n👋 Goodbye!")
            return 0
    if handler == "upload-builtin":
        return run_upload_builtin(args.iso)
    if handler == "upload-large":
        return run_upload_large(args.iso)
    if handler == "automate":
        return run_automate(args)
    if handler == "capture":
        return run_capture(args)
    if handler == "run-sequence":
        return run_sequence(args)

    parser.print_help()
    return 2


if __name__ == "__main__":
    raise SystemExit(main())

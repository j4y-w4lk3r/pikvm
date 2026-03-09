#!/usr/bin/env python3

"""
PiKVM Control TUI
Simple terminal interface for controlling PiKVM ATX power
"""

import requests
import json
import sys
import os
from urllib3.exceptions import InsecureRequestWarning
from pathlib import Path

# Suppress SSL warnings
requests.packages.urllib3.disable_warnings(category=InsecureRequestWarning)

# Load configuration from .env file
def load_env():
    """Load environment variables from .env file"""
    script_dir = Path(__file__).parent
    env_file = script_dir / ".env"
    
    if not env_file.exists():
        print(f"❌ Error: .env file not found at {env_file}")
        sys.exit(1)
    
    config = {}
    with open(env_file, 'r') as f:
        for line in f:
            line = line.strip()
            # Skip empty lines and comments
            if not line or line.startswith('#'):
                continue
            # Parse KEY=VALUE
            if '=' in line:
                key, value = line.split('=', 1)
                config[key.strip()] = value.strip()
    
    required_keys = ['PIKVM_HOST', 'PIKVM_USER', 'PIKVM_PASS']
    for key in required_keys:
        if key not in config:
            print(f"❌ Error: {key} not found in .env file")
            sys.exit(1)
    
    return config

# Load configuration
ENV = load_env()
PIKVM_HOST = ENV['PIKVM_HOST']
PIKVM_USER = ENV['PIKVM_USER']
PIKVM_PASS = ENV['PIKVM_PASS']
BASE_URL = f"https://{PIKVM_HOST}/api"

class PiKVMController:
    def __init__(self):
        self.auth = (PIKVM_USER, PIKVM_PASS)
    
    def get_status(self, port=0):
        """Get the current status of a port"""
        try:
            url = f"{BASE_URL}/switch/atx/state?port={port}"
            response = requests.get(url, auth=self.auth, verify=False, timeout=5)
            if response.status_code == 200:
                data = response.json()
                return data.get('result', {})
            return {"status": "unavailable", "code": response.status_code}
        except Exception as e:
            return {"error": str(e)}
    
    def power_action(self, port=0, action="on"):
        """Turn power on/off"""
        try:
            url = f"{BASE_URL}/switch/atx/power?port={port}&action={action}"
            response = requests.post(url, auth=self.auth, verify=False, timeout=5)
            return response.json()
        except Exception as e:
            return {"error": str(e)}
    
    def click_button(self, port=0, button="power"):
        """Click a button (power, power_long, reset, reset_long)"""
        try:
            url = f"{BASE_URL}/switch/atx/click?port={port}&button={button}"
            response = requests.post(url, auth=self.auth, verify=False, timeout=5)
            return response.json()
        except Exception as e:
            return {"error": str(e)}

def clear_screen():
    os.system('clear' if os.name != 'nt' else 'cls')

def display_menu(controller):
    clear_screen()
    print("=" * 50)
    print("       PiKVM ATX Power Control")
    print("=" * 50)
    print()
    
    # Get current status
    status = controller.get_status(port=0)
    if status and 'error' not in status and 'status' not in status:
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

def main():
    controller = PiKVMController()
    
    while True:
        display_menu(controller)
        choice = input("\nSelect action: ").strip().lower()
        
        if choice == 'q':
            print("\n👋 Goodbye!")
            sys.exit(0)
        
        elif choice == 'r':
            continue
        
        elif choice == '1':
            print("\n⚡ Turning power ON...")
            result = controller.power_action(port=0, action="on")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        
        elif choice == '2':
            print("\n⚡ Turning power OFF...")
            result = controller.power_action(port=0, action="off")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        
        elif choice == '3':
            print("\n🔘 Power Click (short press)...")
            result = controller.click_button(port=0, button="power")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        
        elif choice == '4':
            print("\n🔘 Power Long Press...")
            result = controller.click_button(port=0, button="power_long")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        
        elif choice == '5':
            print("\n🔄 Reset Click (short press)...")
            result = controller.click_button(port=0, button="reset")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        
        elif choice == '6':
            print("\n🔄 Reset Long Press...")
            result = controller.click_button(port=0, button="reset_long")
            print(f"✓ Result: {json.dumps(result, indent=2)}")
            input("\nPress Enter to continue...")
        
        else:
            print("\n❌ Invalid choice")
            input("Press Enter to continue...")

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\n👋 Goodbye!")
        sys.exit(0)


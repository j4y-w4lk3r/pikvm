#!/usr/bin/env python3

"""
PiKVM Large ISO Uploader (No External Dependencies)
Streams large ISO files using only Python built-in libraries
"""

import os
import sys
import ssl
import json
import urllib.request
import urllib.parse
from pathlib import Path
from base64 import b64encode

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
            if not line or line.startswith('#'):
                continue
            if '=' in line:
                key, value = line.split('=', 1)
                config[key.strip()] = value.strip()
    
    return config

class ProgressFileWrapper:
    """Wrapper to show upload progress"""
    def __init__(self, file_obj, total_size):
        self.file = file_obj
        self.total_size = total_size
        self.bytes_read = 0
        self.last_percent = -1
        
    def read(self, size=-1):
        chunk = self.file.read(size)
        if chunk:
            self.bytes_read += len(chunk)
            percent = int((self.bytes_read / self.total_size) * 100)
            
            # Print progress every 5%
            if percent != self.last_percent and percent % 5 == 0:
                mb_read = self.bytes_read // (1024 * 1024)
                mb_total = self.total_size // (1024 * 1024)
                print(f"  📊 Progress: {mb_read} MB / {mb_total} MB ({percent}%)", flush=True)
                self.last_percent = percent
        
        return chunk

def upload_streaming(iso_path, config):
    """Upload ISO using streaming to avoid memory issues"""
    
    if not os.path.exists(iso_path):
        print(f"❌ Error: File not found: {iso_path}")
        return False
    
    filename = os.path.basename(iso_path)
    file_size = os.path.getsize(iso_path)
    file_size_mb = file_size // (1024 * 1024)
    
    host = config['PIKVM_HOST']
    user = config['PIKVM_USER']
    password = config['PIKVM_PASS']
    
    # URL encode the filename
    encoded_filename = urllib.parse.quote(filename)
    url = f"https://{host}/api/msd/write?image={encoded_filename}"
    
    print(f"📀 Uploading {filename}")
    print(f"   Size: {file_size_mb} MB")
    print(f"   Host: {host}")
    print("")
    print("⏳ Starting streaming upload...")
    print("")
    
    try:
        # Create SSL context that doesn't verify certificates
        ssl_context = ssl._create_unverified_context()
        
        # Prepare authentication
        credentials = f"{user}:{password}"
        auth_string = b64encode(credentials.encode()).decode()
        
        # Open file
        with open(iso_path, 'rb') as f:
            # Wrap file with progress tracker
            progress_file = ProgressFileWrapper(f, file_size)
            
            # Create request
            req = urllib.request.Request(
                url,
                data=progress_file,
                headers={
                    'Authorization': f'Basic {auth_string}',
                    'Content-Type': 'application/octet-stream',
                    'Content-Length': str(file_size),
                },
                method='POST'
            )
            
            # Send request
            print("  🚀 Uploading...", flush=True)
            with urllib.request.urlopen(req, context=ssl_context, timeout=3600) as response:
                response_data = response.read().decode()
                result = json.loads(response_data)
                
                print("")
                print("")
                if result.get('ok'):
                    print("✅ Upload successful!")
                    print("")
                    print("Next steps:")
                    print("  1. Verify: ./iso.sh --list")
                    print("  2. Boot: ./pikvm")
                    return True
                else:
                    print(f"❌ Upload failed: {result}")
                    return False
                    
    except urllib.error.HTTPError as e:
        print("")
        print(f"❌ HTTP Error {e.code}: {e.reason}")
        try:
            error_body = e.read().decode()
            print(f"   Details: {error_body}")
        except:
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

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 upload_iso_builtin.py <path-to-iso>")
        print("")
        print("Example:")
        print("  python3 upload_iso_builtin.py /path/to/ubuntu.iso")
        sys.exit(1)
    
    iso_path = sys.argv[1]
    config = load_env()
    
    success = upload_streaming(iso_path, config)
    sys.exit(0 if success else 1)

if __name__ == "__main__":
    main()


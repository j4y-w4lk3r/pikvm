#!/usr/bin/env python3

"""
PiKVM Large ISO Uploader
Streams large ISO files in chunks to avoid memory issues
"""

import os
import sys
import requests
from pathlib import Path
from urllib3.exceptions import InsecureRequestWarning

# Suppress SSL warnings
requests.packages.urllib3.disable_warnings(category=InsecureRequestWarning)

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
    
    url = f"https://{host}/api/msd/write?image={filename}"
    
    print(f"📀 Uploading {filename}")
    print(f"   Size: {file_size_mb} MB")
    print(f"   Host: {host}")
    print("")
    print("⏳ Starting streaming upload...")
    print("")
    
    try:
        # Open file in binary mode and stream it
        with open(iso_path, 'rb') as f:
            # Create a generator that yields chunks
            def file_gen(file_obj, chunk_size=8192):
                """Generator to read file in chunks"""
                total_read = 0
                while True:
                    chunk = file_obj.read(chunk_size)
                    if not chunk:
                        break
                    total_read += len(chunk)
                    # Print progress every 50MB
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
                headers={
                    'Content-Type': 'application/octet-stream',
                },
                timeout=3600  # 1 hour timeout for large files
            )
        
        print("")
        if response.status_code == 200:
            result = response.json()
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
        else:
            print(f"❌ HTTP Error {response.status_code}: {response.text}")
            return False
            
    except requests.exceptions.Timeout:
        print("❌ Upload timed out (file too large or connection too slow)")
        return False
    except Exception as e:
        print(f"❌ Error during upload: {e}")
        return False

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 upload_large_iso.py <path-to-iso>")
        print("")
        print("Example:")
        print("  python3 upload_large_iso.py /path/to/ubuntu.iso")
        sys.exit(1)
    
    iso_path = sys.argv[1]
    config = load_env()
    
    success = upload_streaming(iso_path, config)
    sys.exit(0 if success else 1)

if __name__ == "__main__":
    main()


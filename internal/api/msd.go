package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// IsoEntry is one option in the Boot-from-ISO list.
type IsoEntry struct {
	Display   string `json:"display"`              // e.g. "debian.iso (local)"
	Name      string `json:"name"`                 // filename used for PiKVM set_params
	LocalPath string `json:"local_path,omitempty"` // non-empty = local file (upload first)
}

// SelectMSDImage selects an ISO image as the active mass-storage backing.
func SelectMSDImage(imageName string) error {
	resp, err := Do("POST", "/msd/set_params?image="+imageName, nil, 10*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}
	fmt.Printf("    → API Response: %s\n", string(body))
	return nil
}

// ConnectMSD attaches/detaches the virtual USB drive to/from the target.
func ConnectMSD(connect bool) error {
	v := 0
	if connect {
		v = 1
	}
	resp, err := Do("POST", fmt.Sprintf("/msd/set_connected?connected=%d", v), nil, 10*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}
	fmt.Printf("    → API Response: %s\n", string(body))
	return nil
}

// FetchAvailableISOs returns the list of ISO filenames currently stored on PiKVM.
func FetchAvailableISOs() ([]string, error) {
	resp, err := Do("GET", "/msd", nil, 10*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	var isos []string
	if resultData, ok := result["result"].(map[string]interface{}); ok {
		if storage, ok := resultData["storage"].(map[string]interface{}); ok {
			if images, ok := storage["images"].(map[string]interface{}); ok {
				for name := range images {
					isos = append(isos, name)
				}
			}
		}
	}
	return isos, nil
}

// GetISODir returns the path to the local iso folder (next to executable, cwd fallback).
func GetISODir() string {
	execPath, err := os.Executable()
	if err != nil {
		return filepath.Join(".", "iso")
	}
	dir := filepath.Join(filepath.Dir(execPath), "iso")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	return filepath.Join(".", "iso")
}

// ----------------------------------------------------------------------------
// ISO listing — parallel fetch + 30s TTL cache (roadmap idea #2).
// ----------------------------------------------------------------------------

const isoCacheTTL = 30 * time.Second

type isoCacheEntry struct {
	entries   []IsoEntry
	fetchedAt time.Time
}

var (
	isoCacheMu sync.Mutex
	isoCache   *isoCacheEntry
)

// InvalidateISOCache forces the next FetchAvailableISOEntries call to refetch.
// Called from the WS msd reducer whenever the storage image list changes.
func InvalidateISOCache() {
	isoCacheMu.Lock()
	isoCache = nil
	isoCacheMu.Unlock()
}

// FetchAvailableISOEntries returns ISOs on PiKVM plus local .iso files from
// the iso/ folder. PiKVM and local sources are scanned in parallel; the
// combined result is cached for isoCacheTTL.
//
// Per-source failures are tolerated: if PiKVM is unreachable we still return
// the local entries, and vice-versa. Only an "everything failed" call
// returns an error.
func FetchAvailableISOEntries() ([]IsoEntry, error) {
	isoCacheMu.Lock()
	if isoCache != nil && time.Since(isoCache.fetchedAt) < isoCacheTTL {
		entries := isoCache.entries
		isoCacheMu.Unlock()
		return entries, nil
	}
	isoCacheMu.Unlock()

	var (
		wg           sync.WaitGroup
		pikvmEntries []IsoEntry
		localEntries []IsoEntry
		pikvmErr     error
		localErr     error
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		names, err := FetchAvailableISOs()
		if err != nil {
			pikvmErr = err
			return
		}
		for _, name := range names {
			pikvmEntries = append(pikvmEntries, IsoEntry{Display: name, Name: name, LocalPath: ""})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		isoDir := GetISODir()
		dirEntries, err := os.ReadDir(isoDir)
		if err != nil {
			if !os.IsNotExist(err) {
				localErr = err
			}
			return
		}
		for _, e := range dirEntries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".iso") {
				continue
			}
			fullPath := filepath.Join(isoDir, name)
			if abs, err := filepath.Abs(fullPath); err == nil {
				fullPath = abs
			}
			localEntries = append(localEntries, IsoEntry{
				Display:   name + " (local)",
				Name:      name,
				LocalPath: fullPath,
			})
		}
	}()

	wg.Wait()

	entries := make([]IsoEntry, 0, len(pikvmEntries)+len(localEntries))
	entries = append(entries, pikvmEntries...)
	entries = append(entries, localEntries...)

	if len(entries) == 0 && pikvmErr != nil && localErr != nil {
		return nil, fmt.Errorf("PiKVM: %v; local: %v", pikvmErr, localErr)
	}
	if len(entries) == 0 && pikvmErr != nil {
		return nil, pikvmErr
	}

	isoCacheMu.Lock()
	isoCache = &isoCacheEntry{entries: entries, fetchedAt: time.Now()}
	isoCacheMu.Unlock()

	return entries, nil
}

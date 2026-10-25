// Package openbao reads PiKVM logins from OpenBao KV v2.
//
// Items live at secret/pikvm/pikvm1, secret/pikvm/pikvm2, … with
// username and password fields. The token file can read that path only.
package openbao

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var nameRe = regexp.MustCompile(`^pikvm(\d+)$`)

// HostCreds is one PiKVM login.
type HostCreds struct {
	Name string
	User string
	Pass string
}

// ListPiKVMHosts reads every pikvmN item the token is allowed to see.
func ListPiKVMHosts() ([]HostCreds, error) {
	addr, err := address()
	if err != nil {
		return nil, err
	}
	token, err := token()
	if err != nil {
		return nil, err
	}
	names, err := listNames(addr, token)
	if err != nil {
		return nil, err
	}
	out := make([]HostCreds, 0, len(names))
	for _, name := range names {
		user, pass, err := readItem(addr, token, name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, HostCreds{Name: name, User: user, Pass: pass})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("OpenBao has no secret/pikvm/pikvmN items")
	}
	return out, nil
}

func address() (string, error) {
	if v := strings.TrimSpace(os.Getenv("PIKVM_BAO_ADDR")); v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	text, err := os.ReadFile(configPath("openbao.addr"))
	if err != nil {
		return "", fmt.Errorf("OpenBao address missing (set PIKVM_BAO_ADDR or ~/.config/j4y/openbao.addr)")
	}
	addr := strings.TrimSpace(string(text))
	if addr == "" {
		return "", fmt.Errorf("OpenBao address file is empty")
	}
	return strings.TrimRight(addr, "/"), nil
}

func token() (string, error) {
	path := os.Getenv("PIKVM_BAO_TOKEN_FILE")
	if path == "" {
		path = configPath("openbao-pikvm.token")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("OpenBao token missing (%s)", path)
	}
	tok := strings.TrimSpace(string(text))
	if tok == "" {
		return "", fmt.Errorf("OpenBao token file is empty")
	}
	return tok, nil
}

func configPath(name string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "j4y", name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "j4y", name)
	}
	return filepath.Join(home, ".config", "j4y", name)
}

func listNames(addr, token string) ([]string, error) {
	var body struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := getJSON(addr+"/v1/secret/metadata/pikvm?list=true", token, &body); err != nil {
		return nil, err
	}
	type keyed struct {
		n    int
		name string
	}
	var matches []keyed
	for _, key := range body.Data.Keys {
		name := strings.TrimSuffix(strings.TrimSpace(key), "/")
		m := nameRe.FindStringSubmatch(name)
		if len(m) != 2 {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		matches = append(matches, keyed{n: n, name: name})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].n < matches[j].n })
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.name
	}
	return names, nil
}

func readItem(addr, token, name string) (string, string, error) {
	var body struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := getJSON(addr+"/v1/secret/data/pikvm/"+name, token, &body); err != nil {
		return "", "", err
	}
	user := strings.TrimSpace(body.Data.Data["username"])
	pass := body.Data.Data["password"]
	if user == "" || pass == "" {
		return "", "", fmt.Errorf("missing username or password")
	}
	return user, pass, nil
}

func getJSON(url, token string, dest any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("OpenBao request failed: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenBao returned %d for %s", res.StatusCode, req.URL.Path)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("OpenBao reply was not understood")
	}
	return nil
}

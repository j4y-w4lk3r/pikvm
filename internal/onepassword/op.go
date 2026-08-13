// Package onepassword reads PiKVM credentials from the 1Password CLI (op).
//
// Expected vault items titled pikvm1, pikvm2, … with fields:
//   - web username / web password  (PiKVM web UI — preferred)
//   - or standard Login username / password
package onepassword

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var titleRe = regexp.MustCompile(`(?i)^pikvm(\d+)$`)

// HostCreds is one PiKVM login pulled from 1Password.
type HostCreds struct {
	Name  string // pikvm1, pikvm2, …
	Vault string
	User  string
	Pass  string
}

// itemLister and itemGetter are swappable in tests.
var (
	itemLister = defaultItemLister
	itemGetter = defaultItemGetter
)

func defaultItemLister(vault string) ([]itemRef, error) {
	args := []string{"item", "list", "--format", "json", "--categories", "Login"}
	if vault != "" {
		args = append(args, "--vault", vault)
	}
	out, err := exec.Command("op", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("op item list: %w", err)
	}
	var items []itemRef
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse op item list: %w", err)
	}
	return items, nil
}

func defaultItemGetter(vault, title string) ([]byte, error) {
	args := []string{"item", "get", title, "--format", "json"}
	if vault != "" {
		args = append(args, "--vault", vault)
	}
	out, err := exec.Command("op", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("op item get %q: %w", title, err)
	}
	return out, nil
}

type itemRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Vault struct {
		Name string `json:"name"`
	} `json:"vault"`
}

type opItem struct {
	Title  string    `json:"title"`
	Vault  struct {
		Name string `json:"name"`
	} `json:"vault"`
	Fields []opField `json:"fields"`
}

type opField struct {
	Label   string `json:"label"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
	Value   string `json:"value"`
}

// Available reports whether the op CLI is on PATH.
func Available() bool {
	_, err := exec.LookPath("op")
	return err == nil
}

// ListPiKVMHosts returns credentials for every Login item titled pikvmN.
// Vault is taken from PIKVM_OP_VAULT when set, otherwise all vaults are searched.
func ListPiKVMHosts() ([]HostCreds, error) {
	vault := strings.TrimSpace(getenv("PIKVM_OP_VAULT"))
	items, err := itemLister(vault)
	if err != nil {
		return nil, err
	}

	type keyed struct {
		n    int
		ref  itemRef
	}
	var matches []keyed
	for _, it := range items {
		m := titleRe.FindStringSubmatch(strings.TrimSpace(it.Title))
		if len(m) != 2 {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		matches = append(matches, keyed{n: num, ref: it})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].n < matches[j].n })

	out := make([]HostCreds, 0, len(matches))
	for _, m := range matches {
		v := m.ref.Vault.Name
		if v == "" {
			v = vault
		}
		data, err := itemGetter(v, m.ref.Title)
		if err != nil {
			return nil, err
		}
		creds, err := parseItemJSON(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.ref.Title, err)
		}
		creds.Name = strings.ToLower(m.ref.Title)
		creds.Vault = v
		out = append(out, creds)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no 1Password Login items named pikvm1, pikvm2, … (vault=%q)", vaultOrAll(vault))
	}
	return out, nil
}

func parseItemJSON(data []byte) (HostCreds, error) {
	var item opItem
	if err := json.Unmarshal(data, &item); err != nil {
		return HostCreds{}, fmt.Errorf("parse item json: %w", err)
	}
	user, pass := extractWebCreds(item.Fields)
	if user == "" || pass == "" {
		return HostCreds{}, fmt.Errorf("missing web username/password (or login username/password)")
	}
	return HostCreds{User: user, Pass: pass, Vault: item.Vault.Name}, nil
}

func extractWebCreds(fields []opField) (user, pass string) {
	byLabel := map[string]string{}
	var loginUser, loginPass string
	for _, f := range fields {
		lbl := strings.ToLower(strings.TrimSpace(f.Label))
		val := strings.TrimSpace(f.Value)
		if val == "" {
			continue
		}
		byLabel[lbl] = val
		switch f.Purpose {
		case "USERNAME":
			loginUser = val
		case "PASSWORD":
			loginPass = val
		}
	}
	user = firstNonEmpty(byLabel["web username"], loginUser, byLabel["username"])
	pass = firstNonEmpty(byLabel["web password"], loginPass, byLabel["password"])
	return user, pass
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func vaultOrAll(v string) string {
	if v == "" {
		return "*"
	}
	return v
}

func getenv(k string) string {
	return strings.TrimSpace(os.Getenv(k))
}

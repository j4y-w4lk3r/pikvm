package openbao

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestListPiKVMHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "test-token" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v1/secret/metadata/pikvm":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"keys": []string{"pikvm2", "other", "pikvm1"}},
			})
		case "/v1/secret/data/pikvm/pikvm1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"data": map[string]string{"username": "j4y", "password": "one"}},
			})
		case "/v1/secret/data/pikvm/pikvm2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"data": map[string]string{"username": "j4y", "password": "two"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Setenv("PIKVM_BAO_ADDR", srv.URL)
	t.Setenv("PIKVM_BAO_TOKEN_FILE", filepath.Join(dir, "token"))
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ListPiKVMHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "pikvm1" || got[1].Name != "pikvm2" {
		t.Fatalf("hosts = %+v", got)
	}
	if got[0].User != "j4y" || got[0].Pass != "one" {
		t.Fatalf("pikvm1 = %+v", got[0])
	}
}

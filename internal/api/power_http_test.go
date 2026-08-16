package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPowerBackendURLForAction(t *testing.T) {
	p := &PowerBackend{
		Type:   "http",
		OnURL:  "http://esp/short",
		OffURL: "http://esp/long",
	}
	u, err := p.urlForAction("on")
	if err != nil || u != "http://esp/short" {
		t.Fatalf("on: %q err=%v", u, err)
	}
	u, err = p.urlForAction("off")
	if err != nil || u != "http://esp/long" {
		t.Fatalf("off: %q err=%v", u, err)
	}
}

func TestExecuteHTTPPower(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := &PowerBackend{Type: "http", OnURL: srv.URL + "/short"}
	msg := ExecuteHTTPPower(p, "on")
	if !strings.Contains(msg, "Power on") && !strings.Contains(msg, "\uf00c") {
		t.Fatalf("msg=%q", msg)
	}
	if got != "/short" {
		t.Fatalf("path=%q", got)
	}
}

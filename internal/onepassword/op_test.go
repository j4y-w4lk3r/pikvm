package onepassword

import (
	"encoding/json"
	"testing"
)

func TestExtractWebCreds(t *testing.T) {
	fields := []opField{
		{Label: "username", Purpose: "USERNAME", Value: "ssh-user"},
		{Label: "password", Purpose: "PASSWORD", Value: "ssh-pass"},
		{Label: "web username", Value: "admin"},
		{Label: "web password", Type: "CONCEALED", Value: "web-pass"},
	}
	user, pass := extractWebCreds(fields)
	if user != "admin" || pass != "web-pass" {
		t.Fatalf("got user=%q pass=%q, want admin/web-pass", user, pass)
	}
}

func TestListPiKVMHosts(t *testing.T) {
	itemLister = func(vault string) ([]itemRef, error) {
		p2 := itemRef{Title: "pikvm2"}
		p2.Vault.Name = "lab"
		p1 := itemRef{Title: "pikvm1"}
		p1.Vault.Name = "lab"
		nas := itemRef{Title: "nas"}
		nas.Vault.Name = "lab"
		return []itemRef{p2, p1, nas}, nil
	}
	itemGetter = func(vault, title string) ([]byte, error) {
		item := opItem{Title: title}
		item.Vault.Name = vault
		item.Fields = []opField{
			{Label: "web username", Value: "j4y"},
			{Label: "web password", Value: "secret-" + title},
		}
		return json.Marshal(item)
	}
	t.Cleanup(func() {
		itemLister = defaultItemLister
		itemGetter = defaultItemGetter
	})

	got, err := ListPiKVMHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "pikvm1" || got[1].Name != "pikvm2" {
		t.Fatalf("hosts = %+v, want pikvm1 then pikvm2", got)
	}
	if got[0].User != "j4y" || got[0].Pass != "secret-pikvm1" {
		t.Fatalf("pikvm1 creds = %+v", got[0])
	}
}

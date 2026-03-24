package main

import (
	"os"
	"strings"
	"testing"
)

func TestEncryption(t *testing.T) {
	os.Setenv("PASTEBIN_SERVER_SIDE_ENCRYPTION_ENABLED", "true")
	os.Setenv("PASTEBIN_SERVER_SIDE_ENCRYPTION_KEY", strings.Repeat("a", 32))

	app := newTestApp(t)
	handler := app.router()

	res := doRequest(handler, "POST", "/", strings.NewReader("secret"))
	id := extractID(res.Body.String())

	res = doRequest(handler, "GET", "/raw/"+id, nil)

	if res.Body.String() != "secret" {
		t.Fatal("encryption failed")
	}
}
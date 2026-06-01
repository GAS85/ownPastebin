package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)


func TestJSONMsgHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := &jsonMsgHandler{w: &buf, level: slog.LevelDebug}
	logger := slog.New(h.WithAttrs([]slog.Attr{slog.String("component", "storage"), slog.String("foo", "bar")}))
	logger.Info("hello")
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if out["component"] != "storage" {
		t.Fatalf("expected component storage, got %v", out["component"])
	}
	msg, ok := out["msg"].(map[string]any)
	if !ok || msg["message"] != "hello" || msg["foo"] != "bar" {
		t.Fatalf("unexpected json record: %#v", out)
	}
}

func TestInitLoggerTextAndJSON(t *testing.T) {
	orig := slog.Default()
	defer slog.SetDefault(orig)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	t.Setenv("PASTEBIN_LOG_FORMAT", "json")
	initLogger()
	slog.Info("hello-json")
	w.Close()
	jsonOut, _ := io.ReadAll(r)
	if !strings.Contains(string(jsonOut), "hello-json") {
		t.Fatalf("expected json output, got %q", jsonOut)
	}

	r, w, err = os.Pipe()
	if err != nil {
		t.Fatalf("create second pipe: %v", err)
	}
	os.Stdout = w
	t.Setenv("PASTEBIN_LOG_FORMAT", "text")
	t.Setenv("PASTEBIN_DATE_FORMAT", "2006")
	initLogger()
	slog.Info("hello-text")
	w.Close()
	textOut, _ := io.ReadAll(r)
	if !strings.Contains(string(textOut), "hello-text") {
		t.Fatalf("expected text output, got %q", textOut)
	}
}


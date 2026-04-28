package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// initLogger sets up the global slog logger from environment variables:
//
//	PASTEBIN_LOG_LEVEL       one of DEBUG, INFO, WARN, ERROR  (default: INFO)
//	PASTEBIN_DATE_FORMAT     strftime-style Go time layout for TEXT format only (default: "2006-01-02 15:04:05")
//	                         JSON format always uses RFC3339Nano ("2006-01-02T15:04:05.999999999Z07:00")
//	PASTEBIN_LOG_FORMAT      "json" for structured JSON, anything else for text (default: text)
//
// The output format mirrors the Python/entrypoint.sh style for TEXT format:
//
//	2006-01-02 15:04:05 - INFO - component - message key=value
//
// JSON format uses a custom handler with component hoisted to the top level and
// all remaining attributes nested under "msg":
//
//	{"time":"2006-01-02T15:04:05.000000000Z07:00","level":"INFO","component":"storage","msg":{"message":"sqlite file stats","db_bytes":4096,...}}
func initLogger() {
	level := parseLogLevel(os.Getenv("PASTEBIN_LOG_LEVEL"))
	format := strings.ToLower(strings.TrimSpace(os.Getenv("PASTEBIN_LOG_FORMAT")))

	var handler slog.Handler

	if format == "json" {
		// Custom JSON handler that hoists component to the top level and nests
		// remaining attributes under "msg".
		handler = &jsonMsgHandler{
			w:     os.Stdout,
			level: level,
		}
	} else {
		// Text format with optional user date format
		dateFormat := os.Getenv("PASTEBIN_DATE_FORMAT")
		if dateFormat == "" {
			dateFormat = "2006-01-02 15:04:05"
		}
		handler = &textHandler{
			w:          os.Stdout,
			level:      level,
			dateFormat: dateFormat,
		}
	}

	slog.SetDefault(slog.New(handler))
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// jsonMsgHandler is a custom slog.Handler that writes JSON with:
//   - "time", "level", and "component" as top-level fields
//   - all other attributes nested under a "msg" object alongside "message"
type jsonMsgHandler struct {
	w     io.Writer
	level slog.Level
	attrs []slog.Attr // pre-attached attributes (like component)
}

func (h *jsonMsgHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *jsonMsgHandler) Handle(_ context.Context, r slog.Record) error {
	// Extract component from pre-attached attrs and record attrs, collect the rest.
	component := "pastebin"
	msgObj := make(map[string]interface{})

	for _, attr := range h.attrs {
		attr.Value = attr.Value.Resolve()
		if attr.Key == "component" && attr.Value.Kind() == slog.KindString {
			component = attr.Value.String()
		} else {
			msgObj[attr.Key] = attr.Value.Any()
		}
	}

	msgObj["message"] = r.Message

	r.Attrs(func(a slog.Attr) bool {
		a.Value = a.Value.Resolve()
		if a.Key == "component" && a.Value.Kind() == slog.KindString {
			component = a.Value.String()
		} else {
			msgObj[a.Key] = a.Value.Any()
		}
		return true
	})

	logEntry := map[string]interface{}{
		"time":      r.Time.Format(time.RFC3339Nano),
		"level":     r.Level.String(),
		"component": component,
		"msg":       msgObj,
	}

	encoder := json.NewEncoder(h.w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(logEntry)
}

func (h *jsonMsgHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &jsonMsgHandler{
		w:     h.w,
		level: h.level,
		attrs: newAttrs,
	}
}

func (h *jsonMsgHandler) WithGroup(name string) slog.Handler {
	// Groups are ignored in this handler for simplicity
	return h
}

// textHandler is a custom slog.Handler that writes lines in the format:
// <timestamp> - <LEVEL> - <component> - <message> [key=value ...]
type textHandler struct {
	w          io.Writer
	level      slog.Level
	dateFormat string
	attrs      []slog.Attr
}

func (h *textHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	timestamp := r.Time.Format(h.dateFormat)
	level := levelString(r.Level)

	// Extract component from attributes
	component := "pastebin"
	var attrs strings.Builder

	// Add pre-attached attributes first
	for _, attr := range h.attrs {
		attr.Value = attr.Value.Resolve()
		if attr.Key == "component" && attr.Value.Kind() == slog.KindString {
			component = attr.Value.String()
		} else {
			attrs.WriteByte(' ')
			attrs.WriteString(attr.Key)
			attrs.WriteByte('=')
			attrs.WriteString(attr.Value.String())
		}
	}

	// Then add record attributes
	r.Attrs(func(a slog.Attr) bool {
		a.Value = a.Value.Resolve()
		if a.Key == "component" && a.Value.Kind() == slog.KindString {
			component = a.Value.String()
		} else {
			attrs.WriteByte(' ')
			attrs.WriteString(a.Key)
			attrs.WriteByte('=')
			attrs.WriteString(a.Value.String())
		}
		return true
	})

	_, err := io.WriteString(h.w,
		timestamp+" - "+level+" - "+component+" - "+r.Message+attrs.String()+"\n",
	)
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &textHandler{
		w:          h.w,
		level:      h.level,
		dateFormat: h.dateFormat,
		attrs:      newAttrs,
	}
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	// Groups are ignored in this simple handler
	return h
}

func levelString(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
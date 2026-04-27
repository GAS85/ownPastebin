package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// initLogger sets up the global slog logger from environment variables:
//
//	PASTEBIN_LOG_LEVEL       one of DEBUG, INFO, WARN, ERROR  (default: INFO)
//	PASTEBIN_DATE_FORMAT     strftime-style Go time layout     (default: "2006-01-02 15:04:05")
//	PASTEBIN_LOG_FORMAT      "json" for structured JSON, anything else for text (default: text)
//
// The output format mirrors the Python/entrypoint.sh style:
//
//	2006-01-02 15:04:05 - INFO - main - message key=value
//
// JSON format uses stdlib slog.JSONHandler:
//
//	{"time":"2006-01-02T15:04:05Z","level":"INFO","msg":"message","key":"value"}
func initLogger() {
	level := parseLogLevel(os.Getenv("PASTEBIN_LOG_LEVEL"))

	var handler slog.Handler

	if strings.ToLower(strings.TrimSpace(os.Getenv("PASTEBIN_LOG_FORMAT"))) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					a.Key = "ts"
				}
				if a.Key == slog.LevelKey {
					a.Key = "level"
				}
				return a
			},
		}).WithAttrs([]slog.Attr{
			slog.String("component", "pastebin"),
		})
	} else {
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

// textHandler is a custom slog.Handler that writes lines in the format:
// <timestamp> - <LEVEL> - <source> - <message> [key=value ...]
type textHandler struct {
	w          io.Writer
	level      slog.Level
	dateFormat string
}

func (h *textHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	timestamp := r.Time.Format(h.dateFormat)
	level := levelString(r.Level)

	// Collect key=value attrs
	var attrs strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		attrs.WriteByte(' ')
		attrs.WriteString(a.Key)
		attrs.WriteByte('=')
		attrs.WriteString(a.Value.String())
		return true
	})

	_, err := io.WriteString(h.w,
		timestamp+" - "+level+" - pastebin - "+r.Message+attrs.String()+"\n",
	)
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// For this simple handler we don't need pre-attached attrs —
	// return self so the interface is satisfied.
	return h
}

func (h *textHandler) WithGroup(name string) slog.Handler {
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

// nowForLog returns the current time for log records that don't carry a time
// (e.g. from log.Printf bridge calls). Not used directly but kept for clarity.
func nowForLog() time.Time { return time.Now() }

package logger

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var L *slog.Logger

func init() {
	if os.Getenv("TIDALT_DEBUG") != "true" {
		L = slog.New(slog.NewTextHandler(io.Discard, nil))
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	logDir := filepath.Join(home, ".local", "share", "tidalt")
	_ = os.MkdirAll(logDir, 0o700)

	logFile := filepath.Join(logDir, "debug-"+time.Now().Format("20060102-150405")+".log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		// Fall back to stderr if the file can't be opened.
		L = slog.New(&redactHandler{slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})})
		return
	}

	L = slog.New(&redactHandler{slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})})
	L.Info("debug logging enabled", "file", logFile)
}

// redactHandler wraps a slog.Handler and strips query strings from URL values
// to prevent tokens and other secrets from appearing in log files.
type redactHandler struct {
	inner slog.Handler
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	redacted := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		redacted.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, redacted)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &redactHandler{h.inner.WithAttrs(redacted)}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{h.inner.WithGroup(name)}
}

// redactAttr strips query strings from any string attribute that looks like a URL.
func redactAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() != slog.KindString {
		return a
	}
	v := a.Value.String()
	if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
		return a
	}
	u, err := url.Parse(v)
	if err != nil || u.RawQuery == "" {
		return a
	}
	u.RawQuery = ""
	return slog.String(a.Key, u.String())
}

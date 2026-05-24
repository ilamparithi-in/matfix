package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/ilamparithi-in/matfix/internal/config"
)

// # Logger setup

// Setup configures the default slog logger based on the supplied LoggingConfig.
// After this call all slog.* calls throughout the process use the configured
// level, format, and optional redaction handler.
//
// Setup is idempotent: calling it again replaces the previous default logger.
func Setup(cfg config.LoggingConfig) {
	level := parseLevel(cfg.Level)

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	switch cfg.Format {
	case config.LogFormatJSON:
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	if cfg.Redact {
		handler = &redactingHandler{inner: handler}
	}

	slog.SetDefault(slog.New(handler))
}

// parseLevel converts a lowercase level string to slog.Level.
// Unknown strings default to slog.LevelInfo.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// # Redacting handler

// redactingHandler wraps another slog.Handler and replaces the values of
// known sensitive attribute keys with the literal string "[REDACTED]".
//
// Sensitive keys: "access_token", "token", "key", "password", "secret",
// "authorization", "recovery_key".
type redactingHandler struct {
	inner slog.Handler
}

// sensitiveKeys is the set of attribute keys whose values are scrubbed.
var sensitiveKeys = map[string]bool{
	"access_token":  true,
	"token":         true,
	"key":           true,
	"password":      true,
	"secret":        true,
	"authorization": true,
	"recovery_key":  true,
}

// Enabled delegates to the wrapped handler.
func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle scrubs sensitive attributes in the record before delegating.
func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	scrubbed := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		scrubbed.AddAttrs(scrubAttr(a))
		return true
	})
	return h.inner.Handle(ctx, scrubbed)
}

// WithAttrs returns a new handler with the given attrs pre-applied, scrubbing
// sensitive values first.
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactingHandler{inner: h.inner.WithAttrs(scrubAttrs(attrs))}
}

// WithGroup returns a new handler with the given group name applied.
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name)}
}

// scrubAttrs replaces values of sensitive keys with "[REDACTED]".
func scrubAttrs(attrs []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = scrubAttr(a)
	}
	return out
}

// scrubAttr replaces a single attribute's value with "[REDACTED]" when its key
// is in the sensitive key list.
func scrubAttr(a slog.Attr) slog.Attr {
	if sensitiveKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}

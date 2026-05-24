package observability

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// # Correlation ID context

// correlationIDKey is the unexported context key for the correlation ID.
type correlationIDKey struct{}

// WithCorrelationID returns a child context carrying the given correlation ID.
// Pass an empty string to generate a new random ID.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	if id == "" {
		id = uuid.NewString()
	}
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationIDFromCtx extracts the correlation ID from the context.
// Returns an empty string when none is present.
func CorrelationIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

// LogAttrs returns the slog attributes for the correlation ID stored in ctx.
// When no correlation ID is present the returned slice is empty.
func LogAttrs(ctx context.Context) []slog.Attr {
	if id := CorrelationIDFromCtx(ctx); id != "" {
		return []slog.Attr{slog.String("correlation_id", id)}
	}
	return nil
}

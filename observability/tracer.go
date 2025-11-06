package observability

import (
	"context"

	"github.com/google/uuid"
	"github.com/getsentry/sentry-go"
	"go.uber.org/zap"
)

// TraceIDKey is the context key for trace ID
type traceIDKey struct{}

// WithTraceID adds a trace ID to the context
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey{}, traceID)
}

// TraceIDFromContext extracts trace ID from context
func TraceIDFromContext(ctx context.Context) string {
	if traceID, ok := ctx.Value(traceIDKey{}).(string); ok {
		return traceID
	}
	return ""
}

// NewTraceID generates a new UUID-based trace ID
func NewTraceID() string {
	return uuid.New().String()
}

// InitializeSentry initializes Sentry error tracking
func InitializeSentry(dsn string, environment string) error {
	if dsn == "" {
		zap.L().Info("Sentry DSN not configured, error tracking disabled")
		return nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: environment,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		return err
	}

	zap.L().Info("Sentry initialized successfully")
	return nil
}

// CaptureError sends an error to Sentry
func CaptureError(err error, ctx context.Context) {
	if err == nil {
		return
	}

	traceID := TraceIDFromContext(ctx)
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		if traceID != "" {
			scope.SetTag("trace_id", traceID)
		}
		scope.SetLevel(sentry.LevelError)
	})

	sentry.CaptureException(err)
}

// CaptureMessage sends a message to Sentry
func CaptureMessage(message string, ctx context.Context) {
	traceID := TraceIDFromContext(ctx)
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		if traceID != "" {
			scope.SetTag("trace_id", traceID)
		}
		scope.SetLevel(sentry.LevelInfo)
	})

	sentry.CaptureMessage(message)
}


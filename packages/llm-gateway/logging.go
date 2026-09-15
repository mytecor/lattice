package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// gatewayService is the constant service dimension carried by every structured
// event line (alongside the metric service label). It is stable so a logs
// pipeline (Alloy → Loki) can filter by service without knowing the deployment.
const gatewayService = "llm-gateway"

type logContextKey uint8

const (
	requestIDKey logContextKey = iota
	routeStageKey
	routeAttemptKey
)

func newGatewayLogger(level string) *slog.Logger {
	return newGatewayLoggerTo(level, os.Stdout)
}

func newGatewayLoggerTo(level string, output io.Writer) *slog.Logger {
	var minimum slog.Level
	switch level {
	case "error":
		minimum = slog.LevelError
	case "warn":
		minimum = slog.LevelWarn
	case "info":
		minimum = slog.LevelInfo
	case "debug", "trace":
		minimum = slog.LevelDebug
	default:
		output = io.Discard
		minimum = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: minimum}))
}

// logEvent emits one structured event line. The slog JSON handler already
// renders a single JSON object per line with a timestamp (as "time") and a
// level; logEvent pins the stable event name, the service dimension and the
// low-cardinality routing dimensions (request_id/route_stage/route_attempt)
// into the same object. One line always means one event. Prompt material,
// request/response bodies, headers and API keys are never passed here: the
// caller is responsible for that boundary (see f12-02 DoD).
func logEvent(ctx context.Context, logger *slog.Logger, level slog.Level, event string, extra ...any) {
	attrs := logRequestAttrs(ctx)
	attrs = append(attrs, "service", gatewayService, "event", event)
	attrs = append(attrs, extra...)
	logger.Log(ctx, level, event, attrs...)
}

func withRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

func withRouteAttempt(ctx context.Context, stage, attempt int) context.Context {
	ctx = context.WithValue(ctx, routeStageKey, stage)
	return context.WithValue(ctx, routeAttemptKey, attempt)
}

func logRequestAttrs(ctx context.Context) []any {
	attrs := make([]any, 0, 6)
	if requestID, ok := ctx.Value(requestIDKey).(string); ok && requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	if stage, ok := ctx.Value(routeStageKey).(int); ok && stage > 0 {
		attrs = append(attrs, "route_stage", stage)
	}
	if attempt, ok := ctx.Value(routeAttemptKey).(int); ok && attempt > 0 {
		attrs = append(attrs, "route_attempt", attempt)
	}
	return attrs
}

func safeLogDetail(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maxDetailBytes = 512
	if len(value) > maxDetailBytes {
		value = value[:maxDetailBytes] + "..."
	}
	return value
}

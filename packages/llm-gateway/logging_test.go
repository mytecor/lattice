package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestGatewayLoggerEmitsStructuredContext(t *testing.T) {
	var output bytes.Buffer
	logger := newGatewayLoggerTo("debug", &output)
	ctx := withRouteAttempt(withRequestID(context.Background(), "17"), 2, 3)
	attrs := logRequestAttrs(ctx)
	attrs = append(attrs, "provider", "provider-a")
	logger.Debug("upstream request accepted", attrs...)

	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["request_id"] != "17" || event["route_stage"] != float64(2) || event["route_attempt"] != float64(3) {
		t.Fatalf("missing routing context: %#v", event)
	}
	if event["provider"] != "provider-a" || event["msg"] != "upstream request accepted" {
		t.Fatalf("missing event fields: %#v", event)
	}
}

func TestSilentLoggerEmitsNothing(t *testing.T) {
	var output bytes.Buffer
	newGatewayLoggerTo("silent", &output).Error("must stay silent")
	if output.Len() != 0 {
		t.Fatalf("silent logger wrote %q", output.String())
	}
}

func TestSafeLogDetailIsSingleLineAndBounded(t *testing.T) {
	detail := safeLogDetail("  upstream\n  rejected  " + string(bytes.Repeat([]byte{'x'}, 600)))
	if bytes.ContainsRune([]byte(detail), '\n') {
		t.Fatalf("detail still contains a newline: %q", detail)
	}
	if len(detail) > 515 {
		t.Fatalf("detail was not truncated: %d bytes", len(detail))
	}
}

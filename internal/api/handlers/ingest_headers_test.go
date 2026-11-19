package handlers

import (
	"net/http"
	"testing"
)

func TestCaptureRedactsSensitiveHeaders(t *testing.T) {
	headers := http.Header{"Authorization": {"secret"}, "Cookie": {"session"}, "X-Private": {"value"}, "Content-Type": {"application/json"}}
	captured := extractHeaders(headers, []string{"x-private"})
	for _, key := range []string{"Authorization", "Cookie", "X-Private"} {
		if captured[key] != "[REDACTED]" {
			t.Fatalf("sensitive header retained: %s", key)
		}
	}
	if captured["Content-Type"] != "application/json" {
		t.Fatal("ordinary metadata lost")
	}
}

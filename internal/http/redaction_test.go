package httpclient

import "testing"

func TestRedactorRedactsConfiguredSecrets(t *testing.T) {
	t.Parallel()

	redactor := NewRedactor("token-123", "basic abc")
	value := "authorization failed for token-123 using basic abc"
	got := redactor.Redact(value)

	want := "authorization failed for [REDACTED] using [REDACTED]"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRedactorIgnoresBlankAndDuplicateSecrets(t *testing.T) {
	t.Parallel()

	redactor := NewRedactor("", "token", " token ", "token")
	got := redactor.Redact("token token")
	if got != "[REDACTED] [REDACTED]" {
		t.Fatalf("expected deterministic redaction for duplicates, got %q", got)
	}
}

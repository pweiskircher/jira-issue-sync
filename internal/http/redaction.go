package httpclient

import "strings"

const RedactedPlaceholder = "[REDACTED]"

// Redactor removes sensitive values from error messages.
type Redactor struct {
	secrets []string
}

func NewRedactor(secrets ...string) Redactor {
	if len(secrets) == 0 {
		return Redactor{}
	}

	unique := make([]string, 0, len(secrets))
	seen := make(map[string]struct{}, len(secrets))
	for _, secret := range secrets {
		trimmed := strings.TrimSpace(secret)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}

	return Redactor{secrets: unique}
}

func (r Redactor) Redact(value string) string {
	if value == "" || len(r.secrets) == 0 {
		return value
	}

	redacted := value
	for _, secret := range r.secrets {
		redacted = strings.ReplaceAll(redacted, secret, RedactedPlaceholder)
	}
	return redacted
}

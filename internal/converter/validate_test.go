package converter

import (
	"errors"
	"testing"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

func TestValidateAndCanonicalizeRawADF(t *testing.T) {
	payload := "{\n  \"version\": 1,\n  \"type\": \"doc\",\n  \"content\": []\n}"

	canonical, err := ValidateAndCanonicalizeRawADF(payload)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if canonical != "{\"version\":1,\"type\":\"doc\",\"content\":[]}" {
		t.Fatalf("unexpected canonical payload: %s", canonical)
	}
}

func TestValidateAndCanonicalizeRawADFReturnsTypedErrorForMalformedJSON(t *testing.T) {
	_, err := ValidateAndCanonicalizeRawADF("{bad-json}")
	if err == nil {
		t.Fatalf("expected error")
	}

	if !IsErrorCode(err, ErrorCodeMalformedADFJSON) {
		t.Fatalf("expected malformed JSON converter error, got: %v", err)
	}

	var converterErr *Error
	if !errors.As(err, &converterErr) {
		t.Fatalf("expected typed converter error")
	}
	if converterErr.ReasonCode != contracts.ReasonCodeDescriptionADFBlockMalformed {
		t.Fatalf("unexpected reason code: %s", converterErr.ReasonCode)
	}
}

func TestValidateAndCanonicalizeRawADFReturnsTypedErrorForInvalidEnvelope(t *testing.T) {
	_, err := ValidateAndCanonicalizeRawADF("{\"version\":2,\"type\":\"doc\",\"content\":[]}")
	if err == nil {
		t.Fatalf("expected error")
	}

	if !IsErrorCode(err, ErrorCodeInvalidADFEnvelope) {
		t.Fatalf("expected invalid ADF envelope error, got: %v", err)
	}
}

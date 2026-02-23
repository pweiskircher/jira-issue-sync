package converter

import (
	"errors"
	"fmt"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
)

type ErrorCode string

const (
	ErrorCodeMalformedADFJSON   ErrorCode = "malformed_adf_json"
	ErrorCodeInvalidADFEnvelope ErrorCode = "invalid_adf_envelope"
)

// Error is a typed conversion error with stable reason-code mapping.
type Error struct {
	Code       ErrorCode
	ReasonCode contracts.ReasonCode
	Message    string
	Err        error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsErrorCode(err error, code ErrorCode) bool {
	var converterErr *Error
	if !errors.As(err, &converterErr) {
		return false
	}
	return converterErr.Code == code
}

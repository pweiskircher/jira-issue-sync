package issue

import (
	"errors"
	"fmt"

	"github.com/pat/jira-issue-sync/internal/contracts"
)

type ParseErrorCode string

const (
	ParseErrorCodeMalformedDocument    ParseErrorCode = "malformed_document"
	ParseErrorCodeMalformedFrontMatter ParseErrorCode = "malformed_front_matter"
	ParseErrorCodeUnsupportedField     ParseErrorCode = "unsupported_front_matter_field"
	ParseErrorCodeMissingRequiredField ParseErrorCode = "missing_required_field"
	ParseErrorCodeInvalidSchemaVersion ParseErrorCode = "invalid_schema_version"
	ParseErrorCodeInvalidIssueKey      ParseErrorCode = "invalid_issue_key"
	ParseErrorCodeMalformedRawADF      ParseErrorCode = "malformed_raw_adf"
	ParseErrorCodeInvalidRequiredValue ParseErrorCode = "invalid_required_value"
)

// ParseError is a typed deterministic parser/renderer error.
type ParseError struct {
	Code       ParseErrorCode
	ReasonCode contracts.ReasonCode
	Field      contracts.FrontMatterKey
	Message    string
	Err        error
}

func (e *ParseError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field != "" {
		if e.Err != nil {
			return fmt.Sprintf("%s (%s): %s: %v", e.Code, e.Field, e.Message, e.Err)
		}
		return fmt.Sprintf("%s (%s): %s", e.Code, e.Field, e.Message)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ParseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsParseErrorCode(err error, code ParseErrorCode) bool {
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		return false
	}
	return parseErr.Code == code
}

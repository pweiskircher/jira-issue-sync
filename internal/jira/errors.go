package jira

import (
	"errors"
	"fmt"

	"github.com/pweiskircher/jira-issue-sync/internal/contracts"
	httpclient "github.com/pweiskircher/jira-issue-sync/internal/http"
)

type ErrorCode string

const (
	ErrorCodeInvalidInput     ErrorCode = "invalid_input"
	ErrorCodeRequestEncode    ErrorCode = "request_encode_failed"
	ErrorCodeRequestBuild     ErrorCode = "request_build_failed"
	ErrorCodeTransport        ErrorCode = "transport_error"
	ErrorCodeAuthFailed       ErrorCode = "auth_failed"
	ErrorCodeUnexpectedStatus ErrorCode = "unexpected_status"
	ErrorCodeResponseDecode   ErrorCode = "response_decode_failed"
)

type Error struct {
	Code       ErrorCode
	ReasonCode contracts.ReasonCode
	StatusCode int
	Message    string
	Err        error
	redactor   httpclient.Redactor
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}

	base := err.Message
	if base == "" {
		base = "jira operation failed"
	}
	if err.Err == nil {
		return err.redactor.Redact(base)
	}
	return err.redactor.Redact(fmt.Sprintf("%s: %v", base, err.Err))
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func IsErrorCode(err error, code ErrorCode) bool {
	var jiraErr *Error
	if !errors.As(err, &jiraErr) {
		return false
	}
	return jiraErr.Code == code
}

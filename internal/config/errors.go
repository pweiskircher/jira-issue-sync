package config

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrorCodeReadFailed       ErrorCode = "config_read_failed"
	ErrorCodeParseFailed      ErrorCode = "config_parse_failed"
	ErrorCodeValidationFailed ErrorCode = "config_validation_failed"
	ErrorCodeWriteFailed      ErrorCode = "config_write_failed"
)

type Error struct {
	Code ErrorCode
	Path string
	Err  error
}

func (err *Error) Error() string {
	if err == nil {
		return ""
	}

	var prefix string
	switch err.Code {
	case ErrorCodeReadFailed:
		prefix = "failed to read config"
	case ErrorCodeParseFailed:
		prefix = "failed to parse config"
	case ErrorCodeValidationFailed:
		prefix = "invalid configuration"
	case ErrorCodeWriteFailed:
		prefix = "failed to write config"
	default:
		prefix = "config error"
	}

	if err.Path != "" {
		prefix = fmt.Sprintf("%s at %s", prefix, err.Path)
	}
	if err.Err == nil {
		return prefix
	}
	return fmt.Sprintf("%s: %v", prefix, err.Err)
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func IsErrorCode(err error, code ErrorCode) bool {
	var configErr *Error
	if !errors.As(err, &configErr) {
		return false
	}
	return configErr.Code == code
}

type ResolveErrorCode string

const (
	ResolveErrorCodeInvalidConfig  ResolveErrorCode = "invalid_config"
	ResolveErrorCodeInvalidFlag    ResolveErrorCode = "invalid_flag_value"
	ResolveErrorCodeMissingProfile ResolveErrorCode = "missing_profile"
	ResolveErrorCodeUnknownProfile ResolveErrorCode = "unknown_profile"
	ResolveErrorCodeMissingToken   ResolveErrorCode = "missing_api_token"
)

type ResolveError struct {
	Code    ResolveErrorCode
	Message string
	Err     error
}

func (err *ResolveError) Error() string {
	if err == nil {
		return ""
	}
	if err.Err == nil {
		return "failed to resolve runtime settings: " + err.Message
	}
	return fmt.Sprintf("failed to resolve runtime settings: %s: %v", err.Message, err.Err)
}

func (err *ResolveError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func IsResolveErrorCode(err error, code ResolveErrorCode) bool {
	var resolveErr *ResolveError
	if !errors.As(err, &resolveErr) {
		return false
	}
	return resolveErr.Code == code
}

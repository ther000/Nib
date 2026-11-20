package domain

import (
	"errors"
	"fmt"
)

// 领域通用哨兵错误，供 internal/ 各层与 app/ 薄绑定层使用 errors.Is 统一判定。
var (
	ErrNotFound              = errors.New("not found")
	ErrConflict              = errors.New("conflict")
	ErrValidation            = errors.New("validation failed")
	ErrProviderNotConfigured = errors.New("provider not configured")
	ErrRouteNotConfigured    = errors.New("model route not configured")
	ErrInvalidAPIKey         = errors.New("invalid api key")
	ErrUpstreamRateLimit     = errors.New("upstream rate limited")
	ErrUpstreamUnavailable   = errors.New("upstream unavailable")
	ErrContextOverflow       = errors.New("context window exceeded")
	ErrGenerationCanceled    = errors.New("generation canceled")
	ErrExportDirNotSet       = errors.New("export directory not configured")
)

// ValidationError 表示字段级参数校验失败，并包装 ErrValidation。
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("validation failed: %s", e.Message)
	}
	return fmt.Sprintf("validation failed on %s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrValidation
}

func newValidationError(field, msg string) error {
	return &ValidationError{Field: field, Message: msg}
}

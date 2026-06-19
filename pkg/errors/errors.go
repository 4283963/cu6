package errors

import "fmt"

type Code int

const (
	CodeSuccess          Code = 0
	CodeInvalidParams    Code = 40001
	CodeUnauthorized     Code = 40101
	CodeForbidden        Code = 40301
	CodeNotFound         Code = 40401
	CodeConflict         Code = 40901
	CodeReplayAttack     Code = 42901
	CodeIdempotentLock   Code = 42902
	CodeInternalError    Code = 50001
	CodeDatabaseError    Code = 50002
	CodeCacheError       Code = 50003
	CodeStateTransition  Code = 50004
	CodeMaxRetryExceeded Code = 50005
)

type AppError struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(code Code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func Wrap(code Code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

func InvalidParams(message string) *AppError {
	return New(CodeInvalidParams, message)
}

func Unauthorized(message string) *AppError {
	return New(CodeUnauthorized, message)
}

func Forbidden(message string) *AppError {
	return New(CodeForbidden, message)
}

func NotFound(message string) *AppError {
	return New(CodeNotFound, message)
}

func Conflict(message string) *AppError {
	return New(CodeConflict, message)
}

func ReplayAttack(message string) *AppError {
	return New(CodeReplayAttack, message)
}

func IdempotentLock(message string) *AppError {
	return New(CodeIdempotentLock, message)
}

func InternalError(message string, err error) *AppError {
	return Wrap(CodeInternalError, message, err)
}

func DatabaseError(message string, err error) *AppError {
	return Wrap(CodeDatabaseError, message, err)
}

func CacheError(message string, err error) *AppError {
	return Wrap(CodeCacheError, message, err)
}

func StateTransition(message string) *AppError {
	return New(CodeStateTransition, message)
}

func MaxRetryExceeded(message string) *AppError {
	return New(CodeMaxRetryExceeded, message)
}

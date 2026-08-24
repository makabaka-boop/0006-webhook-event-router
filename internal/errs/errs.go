package errs

import "net/http"

// Code 为可枚举错误码，与 HTTP 状态码一一对应。
type Code string

const (
	CodePayloadInvalid       Code = "payload_invalid"
	CodeSignatureInvalid     Code = "signature_invalid"
	CodeSourceNotFound       Code = "source_not_found"
	CodeEventTypeNotAllowed  Code = "event_type_not_allowed"
	CodeDuplicateEvent       Code = "duplicate_event"
	CodePayloadTooLarge      Code = "payload_too_large"
	CodeTargetNotWhitelisted Code = "target_not_whitelisted"
	CodeNotFound             Code = "not_found"
	CodeInvalidInput         Code = "invalid_input"
	CodeInternal             Code = "internal_error"
	CodeBadRequest           Code = "bad_request"
	CodeReplayRejected       Code = "replay_rejected"
	CodeNotRetryable         Code = "not_retryable"
)

// Error 是带错误码与 HTTP 状态码的类型化错误。
type Error struct {
	Code    Code
	Message string
	Status  int
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// New 构造一个带默认 HTTP 状态映射的错误。
func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg, Status: statusOf(code)}
}

// WithStatus 覆盖默认状态码。
func (e *Error) WithStatus(status int) *Error {
	e.Status = status
	return e
}

func statusOf(c Code) int {
	switch c {
	case CodePayloadInvalid, CodeBadRequest, CodeReplayRejected:
		return http.StatusBadRequest
	case CodeSignatureInvalid:
		return http.StatusUnauthorized
	case CodeSourceNotFound, CodeNotFound:
		return http.StatusNotFound
	case CodeEventTypeNotAllowed:
		return http.StatusUnprocessableEntity
	case CodeDuplicateEvent, CodeNotRetryable:
		return http.StatusConflict
	case CodePayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case CodeTargetNotWhitelisted:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

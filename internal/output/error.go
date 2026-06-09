package output

import "fmt"

// Code is a stable machine-readable error identifier.
type Code string

const (
	CodeUsage    Code = "usage_error"
	CodeConfig   Code = "config_error"
	CodeRemote   Code = "remote_error"
	CodeInternal Code = "internal_error"
)

// Error carries a stable code plus a human-readable message.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Err     error  `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Hint == "" {
		return e.Message
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Hint)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewError creates a Mason error with a stable code.
func NewError(code Code, message string, err error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// WithHint returns a copy of err with a user-facing hint attached.
func WithHint(err *Error, hint string) *Error {
	if err == nil {
		return nil
	}
	next := *err
	next.Hint = hint
	return &next
}

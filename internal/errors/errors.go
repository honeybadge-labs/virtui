// Package errors provides structured error types for virtui.
package errors

import (
	"fmt"

	virtuipb "github.com/rotemtam/virtui/proto/virtui/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// VirtuiError is a structured error with code, category, retryable flag, and suggestion.
type VirtuiError struct {
	Code       string
	Category   virtuipb.ErrorCategory
	Message    string
	Retryable  bool
	Suggestion string
	Context    map[string]string
	GRPCCode   codes.Code
}

func (e *VirtuiError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ToProto converts to protobuf StructuredError.
func (e *VirtuiError) ToProto() *virtuipb.StructuredError {
	return &virtuipb.StructuredError{
		Code:       e.Code,
		Category:   e.Category,
		Message:    e.Message,
		Retryable:  e.Retryable,
		Suggestion: e.Suggestion,
		Context:    e.Context,
	}
}

// ToGRPCStatus converts to a gRPC status with the structured error as detail.
func (e *VirtuiError) ToGRPCStatus() *status.Status {
	st := status.New(e.GRPCCode, e.Message)
	st, err := st.WithDetails(e.ToProto())
	if err != nil {
		// Fallback: return status without details.
		return status.New(e.GRPCCode, e.Message)
	}
	return st
}

// SessionNotFound returns an error for a missing session.
func SessionNotFound(id string) *VirtuiError {
	return &VirtuiError{
		Code:       "SESSION_NOT_FOUND",
		Category:   virtuipb.ErrorCategory_ERROR_CATEGORY_SESSION,
		Message:    fmt.Sprintf("session %q not found", id),
		Retryable:  false,
		Suggestion: "Check the session ID with 'virtui sessions'.",
		Context:    map[string]string{"session_id": id},
		GRPCCode:   codes.NotFound,
	}
}

// SessionNotRunning returns an error for a session whose process has exited.
func SessionNotRunning(id string) *VirtuiError {
	return &VirtuiError{
		Code:       "SESSION_NOT_RUNNING",
		Category:   virtuipb.ErrorCategory_ERROR_CATEGORY_SESSION,
		Message:    fmt.Sprintf("session %q is not running", id),
		Retryable:  false,
		Suggestion: "The process has exited. Start a new session with 'virtui run'.",
		Context:    map[string]string{"session_id": id},
		GRPCCode:   codes.FailedPrecondition,
	}
}

// Timeout returns a timeout error.
func Timeout(operation string, timeoutMs uint32) *VirtuiError {
	return &VirtuiError{
		Code:       "TIMEOUT",
		Category:   virtuipb.ErrorCategory_ERROR_CATEGORY_TIMEOUT,
		Message:    fmt.Sprintf("%s timed out after %dms", operation, timeoutMs),
		Retryable:  true,
		Suggestion: "Increase --timeout or check if the terminal is responsive.",
		Context:    map[string]string{"operation": operation, "timeout_ms": fmt.Sprintf("%d", timeoutMs)},
		GRPCCode:   codes.DeadlineExceeded,
	}
}

// Validation returns a validation error.
func Validation(msg string) *VirtuiError {
	return &VirtuiError{
		Code:       "VALIDATION_ERROR",
		Category:   virtuipb.ErrorCategory_ERROR_CATEGORY_VALIDATION,
		Message:    msg,
		Retryable:  false,
		Suggestion: "Check the command arguments.",
		GRPCCode:   codes.InvalidArgument,
	}
}

// TerminalError returns a terminal-related error.
func TerminalError(msg string) *VirtuiError {
	return &VirtuiError{
		Code:     "TERMINAL_ERROR",
		Category: virtuipb.ErrorCategory_ERROR_CATEGORY_TERMINAL,
		Message:  msg,
		GRPCCode: codes.Internal,
	}
}

// DaemonError returns a daemon-related error.
func DaemonError(msg string) *VirtuiError {
	return &VirtuiError{
		Code:     "DAEMON_ERROR",
		Category: virtuipb.ErrorCategory_ERROR_CATEGORY_DAEMON,
		Message:  msg,
		GRPCCode: codes.Internal,
	}
}

// FromError extracts a VirtuiError from a gRPC status, or returns nil.
func FromError(err error) *VirtuiError {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, detail := range st.Details() {
		if se, ok := detail.(*virtuipb.StructuredError); ok {
			return &VirtuiError{
				Code:       se.Code,
				Category:   se.Category,
				Message:    se.Message,
				Retryable:  se.Retryable,
				Suggestion: se.Suggestion,
				Context:    se.Context,
				GRPCCode:   st.Code(),
			}
		}
	}
	return nil
}

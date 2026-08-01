package v2

import (
	"errors"
	"net/http"
	"strings"
)

const (
	ErrorInvalidJSON                     = "invalid_json"
	ErrorMethodNotAllowed                = "method_not_allowed"
	ErrorNotImplemented                  = "not_implemented"
	ErrorNotFound                        = "not_found"
	ErrorRequestTooLarge                 = "request_too_large"
	ErrorRequestFailed                   = "request_failed"
	ErrorResponseTooLarge                = "response_too_large"
	ErrorUnauthorized                    = "unauthorized"
	ErrorOwnerRequired                   = "owner_required"
	ErrorAdministratorRequired           = "administrator_required"
	ErrorVersionRequired                 = "ipc_version_required"
	ErrorProtocolUnsupported             = "ipc_protocol_unsupported"
	ErrorVersionUnsupported              = "ipc_version_unsupported"
	ErrorInvalidVersionRange             = "invalid_ipc_version_range"
	ErrorRemoteCleanupRequired           = "remote_cleanup_required"
	ErrorLocalForgetConfirmationRequired = "local_forget_confirmation_required"
	ErrorRecoveryOperationInvalid        = "recovery_operation_invalid"
	ErrorRecoveryOperationIDFailed       = "recovery_operation_id_failed"
	ErrorLocalForgetFailed               = "local_forget_failed"
)

type Error struct {
	Code      string
	Err       error
	Status    int
	RequestID string
}

func NewErrorWithRequestID(status int, code, requestID string, err error) Error {
	ipcErr := NewError(status, code, err)
	ipcErr.RequestID = strings.TrimSpace(requestID)
	return ipcErr
}

func (e Error) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}

func NewError(status int, code string, err error) Error {
	if strings.TrimSpace(code) == "" {
		code = ErrorRequestFailed
	}
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if err == nil {
		err = errors.New(code)
	}
	return Error{Code: code, Err: err, Status: status}
}

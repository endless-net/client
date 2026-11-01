package clientapp

import "github.com/endless-net/client/internal/control"

// NewApprovalEnrollmentWorker binds the explicit browser class to the same
// application providers. A product command must separately persist that class.
func (t *Telemetry) NewApprovalEnrollmentWorker(login *control.LoginExecutor, registration *control.RegistrationExecutor, approval *control.ApprovalExecutor) (*control.LoginWorker, error) {
	if t == nil || !t.signals.valid() {
		return nil, ErrSignals
	}
	return control.NewApprovalEnrollmentWorker(login, registration, approval, t.traces.Tracer("endlessnet/client"), t.metrics.Meter("endlessnet/client"), t.signals.logger)
}

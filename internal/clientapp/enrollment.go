package clientapp

import (
	"net/http"

	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
)

func (t *Telemetry) enrollmentWorkers(store *state.Store, httpClient *http.Client, bootstrap *control.Bootstrap, loginConfig control.LoginConfig, registrationConfig control.RegistrationConfig, approvalBase string) (*control.LoginWorker, *control.SessionLoginWorker, error) {
	native, err := control.NewNative(httpClient, bootstrap, registrationConfig.MaxResponseBytes, registrationConfig.Now)
	if err != nil {
		return nil, nil, err
	}
	login, err := control.NewLoginExecutor(store, native, loginConfig)
	if err != nil {
		return nil, nil, err
	}
	sessionLogin, err := t.NewSessionLoginWorker(store, native, loginConfig)
	if err != nil {
		return nil, nil, err
	}
	registration, err := control.NewRegistration(httpClient, bootstrap, registrationConfig)
	if err != nil {
		return nil, nil, err
	}
	executor := control.RegistrationExecutorConfig{
		MaxCommitRetries: loginConfig.MaxCommitRetries, RetryInterval: loginConfig.RetryInterval,
		WriteBudget: loginConfig.WriteBudget, Now: loginConfig.Now,
	}
	register, err := control.NewRegistrationExecutor(store, registration, executor)
	if err != nil {
		return nil, nil, err
	}
	approval, err := control.NewApproval(httpClient, bootstrap, control.ApprovalConfig{RegistrationConfig: registrationConfig, ApprovalBase: approvalBase})
	if err != nil {
		return nil, nil, err
	}
	approve, err := control.NewApprovalExecutor(store, approval, control.ApprovalExecutorConfig(executor))
	if err != nil {
		return nil, nil, err
	}
	defaultTarget, err := control.NewDefaultTarget(httpClient, bootstrap, registrationConfig)
	if err != nil {
		return nil, nil, err
	}
	resolve, err := control.NewDefaultTargetExecutor(store, defaultTarget, executor)
	if err != nil {
		return nil, nil, err
	}
	worker, err := t.NewDefaultEnrollmentWorker(login, register, approve, resolve)
	return worker, sessionLogin, err
}

func (t *Telemetry) NewSessionLoginWorker(store *state.Store, native *control.Native, config control.LoginConfig) (*control.SessionLoginWorker, error) {
	if t == nil || !t.signals.valid() {
		return nil, ErrSignals
	}
	return control.NewSessionLoginWorker(store, native, config, t.traces.Tracer("endlessnet/client"), t.metrics.Meter("endlessnet/client"), t.signals.logger)
}

// NewApprovalEnrollmentWorker binds the explicit browser class to the same
// application providers. A product command must separately persist that class.
func (t *Telemetry) NewApprovalEnrollmentWorker(login *control.LoginExecutor, registration *control.RegistrationExecutor, approval *control.ApprovalExecutor) (*control.LoginWorker, error) {
	if t == nil || !t.signals.valid() {
		return nil, ErrSignals
	}
	return control.NewApprovalEnrollmentWorker(login, registration, approval, t.traces.Tracer("endlessnet/client"), t.metrics.Meter("endlessnet/client"), t.signals.logger)
}

func (t *Telemetry) NewDefaultEnrollmentWorker(login *control.LoginExecutor, registration *control.RegistrationExecutor, approval *control.ApprovalExecutor, target *control.DefaultTargetExecutor) (*control.LoginWorker, error) {
	if t == nil || !t.signals.valid() {
		return nil, ErrSignals
	}
	return control.NewDefaultEnrollmentWorker(login, registration, approval, target, t.traces.Tracer("endlessnet/client"), t.metrics.Meter("endlessnet/client"), t.signals.logger)
}

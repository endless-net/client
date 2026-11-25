package dataplane

import "errors"

var (
	ErrAdapterPermission = errors.New("tunnel adapter permission denied")
	ErrAdapterConflict   = errors.New("tunnel adapter conflicts with existing interface")
	ErrAdapterOwnership  = errors.New("tunnel adapter ownership changed")
	ErrIPPermission      = errors.New("IP configuration permission denied")
	ErrIPConflict        = errors.New("IP configuration conflict")
	ErrIPOwnership       = errors.New("IP configuration ownership changed")
	ErrPolicyPermission  = errors.New("packet policy permission denied")
	ErrPolicyOwnership   = errors.New("packet policy ownership changed")
)

//go:build !linux && !windows

package dataplane

var platformDirectPreparer kernelPreparer

package main

import (
	"fmt"
	"strings"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func nativeCLIEnrollment(mode, hostname, token string, browser bool) (*ipc.EnrollRequest, error) {
	modes := map[string]ipc.EnrollmentMode{
		"workstation":   ipc.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION,
		"server":        ipc.EnrollmentMode_ENROLLMENT_MODE_SERVER,
		"subnet-router": ipc.EnrollmentMode_ENROLLMENT_MODE_SUBNET_ROUTER,
		"interactive":   ipc.EnrollmentMode_ENROLLMENT_MODE_INTERACTIVE,
	}
	selected, ok := modes[mode]
	if !ok {
		return nil, fmt.Errorf("--mode must be workstation, server, subnet-router or interactive")
	}
	if (strings.TrimSpace(token) != "") == browser {
		return nil, fmt.Errorf("enroll requires exactly one of --enrollment-token-file or --browser-login")
	}
	request := &ipc.EnrollRequest{Mode: selected, Hostname: hostname}
	if browser {
		request.Authentication = &ipc.EnrollRequest_BrowserLogin{BrowserLogin: true}
	} else {
		request.Authentication = &ipc.EnrollRequest_EnrollmentToken{EnrollmentToken: token}
	}
	return request, nil
}

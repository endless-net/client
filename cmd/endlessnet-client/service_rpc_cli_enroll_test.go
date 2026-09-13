package main

import (
	"bytes"
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestNativeCLIEnrollmentAuthenticationAndModes(t *testing.T) {
	for _, mode := range []string{"workstation", "server", "subnet-router", "interactive"} {
		for _, browser := range []bool{true, false} {
			token := "synthetic-cli-token"
			if browser {
				token = ""
			}
			request, err := nativeCLIEnrollment(mode, "cross-platform-host", token, browser)
			if err != nil {
				t.Fatal(err)
			}
			if request.Mode == ipc.EnrollmentMode_ENROLLMENT_MODE_UNSPECIFIED || request.Hostname != "cross-platform-host" || request.GetBrowserLogin() != browser || request.GetEnrollmentToken() != token {
				t.Fatal("native enrollment payload lost mode/authentication")
			}
			encoded, err := proto.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			decoded := new(ipc.EnrollRequest)
			if err := proto.Unmarshal(encoded, decoded); err != nil || !proto.Equal(request, decoded) {
				t.Fatal("authentication oneof did not round-trip")
			}
		}
	}
	for _, input := range []struct {
		mode, token string
		browser     bool
	}{{"unknown", "synthetic-cli-token", false}, {"server", "synthetic-cli-token", true}, {"interactive", "", false}} {
		_, err := nativeCLIEnrollment(input.mode, "host", input.token, input.browser)
		if err == nil || strings.Contains(err.Error(), "synthetic-cli-token") {
			t.Fatal("invalid authentication accepted or exposed")
		}
	}
}

func TestNativeCLIEnrollmentRejectsAmbiguousAuthBeforeReadingFile(t *testing.T) {
	var output bytes.Buffer
	err := cmdServiceRPCMutation("enroll", []string{"--request-id", "728b7bd6-ab32-40f2-8585-a4b48d24ef47", "--expected-instance-id", "instance", "--expected-revision", "1", "--profile-id", "profile", "--mode", "interactive", "--browser-login", "--enrollment-token-file", "must-not-read"}, &output)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") || output.Len() != 0 {
		t.Fatal("ambiguous authentication reached token I/O", err)
	}
}

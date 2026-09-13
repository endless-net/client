package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdateInfoRejectsInvalidCallerIdentityBeforeTransport(t *testing.T) {
	for _, value := range []string{"{", `{"unknown":true}`, `{"commit":"a","commit":"b"}`, strings.Repeat("x", 4097)} {
		var output bytes.Buffer
		err := cmdServiceRPCQuery("update-info", []string{"--reported-ui", value}, &output)
		if err == nil || err.Error() != "--reported-ui requires bounded BuildIdentity protobuf JSON" || output.Len() != 0 {
			t.Fatal("invalid caller claim reached transport or exposed input")
		}
	}
}

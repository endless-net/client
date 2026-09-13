package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNativeResourceMutationRequiresExplicitIntentAndContext(t *testing.T) {
	base := []string{"--profile-id", "profile", "--request-id", "b10b8dab-f1a2-46a0-b489-0e151c2bcc51", "--expected-instance-id", "instance", "--expected-revision", "7"}
	for _, args := range [][]string{nil, {"--resource-id", "resource"}, {"--enabled=false"}, {"--resource-id", " ", "--enabled=true"}} {
		var output bytes.Buffer
		err := cmdServiceRPCMutation("set-resource-enabled", append(append([]string(nil), base...), args...), &output)
		if err == nil || !strings.Contains(err.Error(), "requires --resource-id and explicit") || output.Len() != 0 {
			t.Fatal("resource mutation reached transport without explicit intent")
		}
	}
	var output bytes.Buffer
	if err := cmdServiceRPCMutation("set-resource-enabled", []string{"--resource-id", "resource", "--enabled=false"}, &output); err == nil || !strings.Contains(err.Error(), "--request-id UUID") || output.Len() != 0 {
		t.Fatal("resource mutation accepted missing durable context")
	}
}

package testclient

import (
	"strings"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNativeResponseDecoderDoesNotAcceptLegacyOrExposeOutput(t *testing.T) {
	for _, raw := range []string{`{"state":"connected","private_key":"synthetic-private"}`, `malformed synthetic-private`, `{"status":{"connectionPhase":"INVALID_ENUM"}}`} {
		err := decodeNativeService([]byte(raw), &ipc.GetStatusResponse{})
		if err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("invalid response accepted or exposed")
		}
	}
	response := &ipc.GetStatusResponse{}
	if err := decodeNativeService([]byte(`{"status":{"nodeId":"node","intent":{"desiredState":"DESIRED_STATE_DISCONNECTED"},"metadata":{"instanceId":"instance","revision":"7"}}}`), response); err != nil {
		t.Fatal(err)
	}
	if response.Status.NodeId != "node" || response.Status.GetIntent().DesiredState != ipc.DesiredState_DESIRED_STATE_DISCONNECTED || response.Status.Metadata.Revision != 7 {
		t.Fatal("native semantics lost")
	}
	if err := decodeNativeService([]byte(`{}`), nil); err == nil {
		t.Fatal("nil target accepted")
	}
}

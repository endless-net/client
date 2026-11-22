package control

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"google.golang.org/protobuf/proto"
)

func readRPC(t *testing.T, request *http.Request, message proto.Message) []byte {
	t.Helper()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/proto" || request.Header.Get("Connect-Protocol-Version") != "1" {
		t.Fatal("expected a generated unary Connect request")
	}
	if err := proto.Unmarshal(body, message); err != nil {
		t.Fatal(err)
	}
	return body
}

func rpcResponse(t *testing.T, request *http.Request, message proto.Message) *http.Response {
	t.Helper()
	body, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/proto"}},
		Body: io.NopCloser(bytes.NewReader(body)), Request: request}
}

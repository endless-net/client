package client

import (
	"runtime"
	"testing"

	"connectrpc.com/connect"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCSupportInfoIsOfflineAndImmutable(t *testing.T) {
	build := &ipc.BuildIdentity{Version: "test-version", Commit: "test-commit", BuildDate: "test-date"}
	service := NewClientRPCService(newRPCStoreTest(t), build)
	build.Version = "modified input"
	for i := 0; i < 2; i++ {
		response, err := service.GetSupportInfo(t.Context(), connect.NewRequest(&ipc.GetSupportInfoRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		info := response.Msg.Info
		if info.ProductName != "EndlessNet" || info.Runtime.Version != "test-version" || info.Runtime.Commit != "test-commit" || info.Runtime.BuildDate != "test-date" || info.Runtime.Architecture != runtime.GOARCH {
			t.Fatal("support build identity was lost or mutated")
		}
		if info.DocumentationUrl != "" || info.SupportUrl != "" || info.PrivacyUrl != "" || info.LicenseUrl != "" || info.OfflineHelpKey != "" {
			t.Fatal("unverified support destination advertised")
		}
		info.Runtime.Version = "modified response"
	}
}

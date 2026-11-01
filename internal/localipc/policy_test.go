package localipc

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestPublishedMethodAuthorization(t *testing.T) {
	for _, test := range []struct {
		method, owner, sid string
		admin              bool
		want               codes.Code
	}{
		{"GetRuntimeInfo", "owner", "observer", false, codes.OK},
		{"GetEnrollmentAttempt", "owner", "observer", false, codes.PermissionDenied},
		{"GetEnrollmentAttempt", "owner", "owner", false, codes.OK},
		{"GetEnrollmentAttempt", "", "caller", false, codes.PermissionDenied},
		{"Enroll", "", "caller", false, codes.OK},
		{"CreateProfile", "", "caller", false, codes.OK},
		{"CancelEnrollmentAttempt", "", "caller", false, codes.PermissionDenied},
		{"SelectEnrollmentTarget", "owner", "other", true, codes.PermissionDenied},
		{"TrustServerIdentity", "owner", "owner", false, codes.PermissionDenied},
		{"TrustServerIdentity", "owner", "administrator", true, codes.OK},
		{"Unknown", "owner", "owner", true, codes.PermissionDenied},
	} {
		t.Run(test.method+"/"+test.owner+"/"+test.sid, func(t *testing.T) {
			ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: AuthInfo{principal: Principal{SID: test.sid, Administrator: test.admin}}})
			err := Authorize(ctx, "/client.v0.ClientService/"+test.method, func(context.Context) (string, error) { return test.owner, nil })
			if status.Code(err) != test.want {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}

func TestPublishedRuntimeBootstrapVersionZero(t *testing.T) {
	digest := [32]byte{1}
	info := &clientv0.RuntimeInfo{InstanceId: "instance", Protocol: "endlessnet-client-ipc", IpcVersion: 0, ContractSha256: hex.EncodeToString(digest[:])}
	if err := CheckRuntimeInfo(info, "instance", digest); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []*clientv0.RuntimeInfo{{InstanceId: "other", Protocol: info.Protocol, ContractSha256: info.ContractSha256}, {InstanceId: info.InstanceId, Protocol: info.Protocol, IpcVersion: 1, ContractSha256: info.ContractSha256}, {InstanceId: info.InstanceId, Protocol: info.Protocol, ContractSha256: "wrong"}} {
		if err := CheckRuntimeInfo(changed, "instance", digest); status.Code(err) != codes.FailedPrecondition {
			t.Fatal("invalid bootstrap accepted", err)
		}
	}
}

func TestMetadataCannotSupplyOSIdentity(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("sid", "owner", "administrator", "true"))
	if err := Authorize(ctx, "/client.v0.ClientService/TrustServerIdentity", nil); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("metadata authenticated caller: %v", err)
	}
	ctx = peer.NewContext(context.Background(), &peer.Peer{AuthInfo: AuthInfo{principal: Principal{SID: "owner"}}})
	if err := Authorize(ctx, "/client.v0.ClientService/Enroll", func(context.Context) (string, error) { return "", errors.New("store failed") }); status.Code(err) != codes.Unavailable {
		t.Fatalf("store outage allowed claim: %v", err)
	}
}

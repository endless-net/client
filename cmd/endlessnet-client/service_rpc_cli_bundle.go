package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func cmdServiceRPCBundleExport(args []string, output io.Writer) error {
	fs := flag.NewFlagSet("service export-diagnostics-bundle", flag.ContinueOnError)
	pipe, socket := serviceIPCTransportFlags(fs)
	operationID := fs.String("operation-id", "", "required completed bundle operation UUID")
	profileID := fs.String("profile-id", "", "required owning profile")
	timeoutValue := fs.String("timeout", "30s", "maximum time to verify and read the archive")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !nativeRequestUUID(*operationID) || strings.TrimSpace(*profileID) == "" {
		return fmt.Errorf("export requires --operation-id UUID and --profile-id, without positional arguments")
	}
	timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
	if err != nil {
		return err
	}
	endpoint, err := nativeServiceEndpoint(*pipe, *socket)
	if err != nil {
		return err
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		return err
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if _, err := consumer.Bootstrap(ctx); err != nil {
		return err
	}
	response, err := consumer.GetOperation(ctx, connect.NewRequest(&ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: *operationID}}))
	if err != nil {
		return err
	}
	op := response.Msg.Operation
	if op.GetId() != *operationID || op.GetProfileId() != *profileID || op.GetKind() != ipc.OperationKind_OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE || op.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.GetBundle() == nil {
		return fmt.Errorf("operation does not identify a successful bundle for this profile")
	}
	data, err := readNativeDiagnosticsBundle(ctx, op.GetBundle(), time.Now, func(ctx context.Context, request *ipc.ReadDiagnosticsBundleRequest) (*ipc.ReadDiagnosticsBundleResponse, error) {
		response, err := consumer.ReadDiagnosticsBundle(ctx, connect.NewRequest(request))
		if err != nil {
			return nil, err
		}
		return response.Msg, nil
	})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := output.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func readNativeDiagnosticsBundle(ctx context.Context, source *ipc.BundleResult, now func() time.Time, read func(context.Context, *ipc.ReadDiagnosticsBundleRequest) (*ipc.ReadDiagnosticsBundleResponse, error)) ([]byte, error) {
	if source == nil {
		return nil, fmt.Errorf("bundle metadata is missing")
	}
	bundle := proto.Clone(source).(*ipc.BundleResult)
	expected, err := hex.DecodeString(bundle.Sha256)
	if err != nil || len(expected) != sha256.Size || bundle.BundleId == "" || len(bundle.BundleId) > 256 || bundle.SizeBytes == 0 || bundle.SizeBytes > 5<<20 || bundle.CreatedAt == nil || bundle.ExpiresAt == nil || bundle.CreatedAt.CheckValid() != nil || bundle.ExpiresAt.CheckValid() != nil {
		return nil, fmt.Errorf("invalid bundle metadata")
	}
	created, expires := bundle.CreatedAt.AsTime(), bundle.ExpiresAt.AsTime()
	if !expires.After(created) || expires.Sub(created) > 15*time.Minute {
		return nil, fmt.Errorf("invalid bundle lifetime")
	}
	data := make([]byte, 0, int(bundle.SizeBytes))
	for uint64(len(data)) < bundle.SizeBytes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !now().Before(expires) {
			return nil, fmt.Errorf("bundle expired")
		}
		offset := uint64(len(data))
		response, err := read(ctx, &ipc.ReadDiagnosticsBundleRequest{BundleId: bundle.BundleId, Offset: offset, MaxBytes: 64 << 10})
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !now().Before(expires) {
			return nil, fmt.Errorf("bundle expired")
		}
		if response == nil || len(response.Data) == 0 || len(response.Data) > 64<<10 || uint64(len(response.Data)) > bundle.SizeBytes-offset || response.NextOffset != offset+uint64(len(response.Data)) || response.Eof != (response.NextOffset == bundle.SizeBytes) {
			return nil, fmt.Errorf("invalid bundle chunk")
		}
		data = append(data, response.Data...)
	}
	digest := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), bundle.Sha256) {
		return nil, fmt.Errorf("bundle digest mismatch")
	}
	return data, nil
}

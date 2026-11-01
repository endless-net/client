//go:build windows

package localipc

import (
	"context"
	"errors"
	"net"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/grpc"
)

// NewServer wires mandatory OS credentials and both unary/stream authorization.
// Implementation must still enforce immutable revisions, atomic ownership claim,
// and exact-current-announcement trust. No unauthenticated gRPC server is exposed.
func NewServer(config PipeConfig, owner OwnerReader, implementation clientv0.ClientServiceServer, maxMessageBytes int, maxConcurrent uint32, observation Observation) (*grpc.Server, error) {
	if owner == nil || implementation == nil || maxMessageBytes <= 0 || maxConcurrent == 0 || observation.Unary == nil || observation.Stream == nil {
		return nil, errPipe
	}
	creds, err := Credentials(config)
	if err != nil {
		return nil, err
	}
	server := grpc.NewServer(grpc.Creds(creds), grpc.ChainUnaryInterceptor(observation.Unary, Unary(owner)), grpc.ChainStreamInterceptor(observation.Stream, Stream(owner)), grpc.MaxConcurrentStreams(maxConcurrent), grpc.MaxRecvMsgSize(maxMessageBytes), grpc.MaxSendMsgSize(maxMessageBytes))
	clientv0.RegisterClientServiceServer(server, implementation)
	return server, nil
}

// Connect exposes a channel only after the existing GetRuntimeInfo bootstrap
// validates the exact instance/protocol/version/digest. It uses no custom
// transport bytes and no compatibility fallback.
func Connect(ctx context.Context, config PipeConfig) (*grpc.ClientConn, error) {
	creds, err := Credentials(config)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient("passthrough:///endlessnet-local", grpc.WithTransportCredentials(creds), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return Dial(ctx, config) }))
	if err != nil {
		return nil, err
	}
	operation, cancel := context.WithTimeout(ctx, config.HandshakeTimeout)
	defer cancel()
	info, err := clientv0.NewClientServiceClient(conn).GetRuntimeInfo(operation, &clientv0.GetRuntimeInfoRequest{})
	if err == nil {
		err = CheckRuntimeInfo(info.Runtime, config.InstanceID, config.ContractDigest)
	}
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	return conn, nil
}

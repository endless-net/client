package client

import (
	"context"

	ipc "github.com/endless-net/client/clientipc/v0"
)

// Runtime-only provider contract. OperationID is the durable selection ID;
// registration checkpoints go exclusively to its prepared target.
type ClientRPCNetworkRegistrationInput struct {
	OperationID string
	NetworkID   string
	Hostname    string
	Tags        []string
}

type ClientRPCNetworkRegistrationProvider func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) (*ipc.UserAction, error)

// ClientRPCNetworkTargetCleanupProvider compensates an isolated registration.
// It must resolve an uncertain saved request before revoking its node, checkpoint
// recovered authority before revocation, and never revoke the shared session.
type ClientRPCNetworkTargetCleanupProvider func(context.Context, Config, ClientRPCNetworkRegistrationInput, func(Config) error) error

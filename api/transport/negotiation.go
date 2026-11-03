package transport

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
)

const (
	Challenge = "challenge"
	Echo      = "echo"
	Prepare   = "prepare"
	Prepared  = "prepared"
	Commit    = "commit"
	Committed = "committed"
)

// Control travels only within authenticated peer crypto data, is intercepted
// before delivery to the OS, and is never a grant. All bindings must match the
// current pair/candidate at the receiver; validation here checks shape only.
// Generations identify each side's current connection intent. CandidateID and
// Nonce prevent old replies from confirming a new isolated transport candidate.
type Control struct {
	Version               uint32 `json:"version"`
	Kind                  string `json:"kind"`
	AccountID             string `json:"account_id"`
	NetworkID             string `json:"network_id"`
	SourceNodeID          string `json:"source_node_id"`
	DestinationNodeID     string `json:"destination_node_id"`
	SourcePublicKey       string `json:"source_public_key"`
	DestinationPublicKey  string `json:"destination_public_key"`
	SourceGeneration      uint64 `json:"source_generation"`
	DestinationGeneration uint64 `json:"destination_generation"`
	NetworkRevision       uint64 `json:"network_revision"`
	ProfileDigest         string `json:"profile_digest"`
	CandidateID           string `json:"candidate_id"`
	Nonce                 []byte `json:"nonce"`
}

func (Control) String() string   { return "[peer transport control]" }
func (Control) GoString() string { return "[peer transport control]" }

func ValidateControl(c Control) error {
	if c.Version != Version {
		return ErrUnsupported
	}
	switch c.Kind {
	case Challenge, Echo, Prepare, Prepared, Commit, Committed:
	default:
		return ErrUnsupported
	}
	if !identifier(c.AccountID) || !identifier(c.NetworkID) || !identifier(c.SourceNodeID) || !identifier(c.DestinationNodeID) || c.SourceNodeID == c.DestinationNodeID || !validPublicKey(c.SourcePublicKey) || !validPublicKey(c.DestinationPublicKey) || c.SourcePublicKey == c.DestinationPublicKey || c.SourceGeneration == 0 || c.DestinationGeneration == 0 || c.NetworkRevision == 0 || !ValidDigest(c.ProfileDigest) || !validCandidate(c.CandidateID) || len(c.Nonce) != 32 || bytes.Equal(c.Nonce, make([]byte, 32)) {
		return ErrInvalid
	}
	if (c.Kind == Prepare || c.Kind == Commit) && c.SourceNodeID > c.DestinationNodeID ||
		(c.Kind == Prepared || c.Kind == Committed) && c.SourceNodeID < c.DestinationNodeID {
		return ErrInvalid
	}
	return nil
}

func validCandidate(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 16 && hex.EncodeToString(raw) == value && !bytes.Equal(raw, make([]byte, 16))
}

func validPublicKey(value string) bool {
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	return err == nil && len(raw) == 32 && base64.StdEncoding.EncodeToString(raw) == value && !bytes.Equal(raw, make([]byte, 32))
}

func DecodeControl(raw []byte) (Control, error) {
	var c Control
	if decode(raw, MaxDatagramBytes, &c) != nil {
		return Control{}, ErrInvalid
	}
	if err := ValidateControl(c); err != nil {
		return Control{}, err
	}
	return c, nil
}

func MarshalControl(c Control) ([]byte, error) {
	if err := ValidateControl(c); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > MaxDatagramBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}

// MatchesReply binds a reply to the exact original attempt, including reflected
// identities/generations, profile and nonce. This does not authenticate its sender
// or replace current authorization, isolated path evidence or replay tracking.
func MatchesReply(request, reply Control) bool {
	if ValidateControl(request) != nil || ValidateControl(reply) != nil {
		return false
	}
	wanted := map[string]string{Challenge: Echo, Prepare: Prepared, Commit: Committed}[request.Kind]
	return wanted != "" && reply.Kind == wanted && request.AccountID == reply.AccountID &&
		request.NetworkID == reply.NetworkID && request.SourceNodeID == reply.DestinationNodeID &&
		request.DestinationNodeID == reply.SourceNodeID && request.SourcePublicKey == reply.DestinationPublicKey &&
		request.DestinationPublicKey == reply.SourcePublicKey && request.SourceGeneration == reply.DestinationGeneration &&
		request.DestinationGeneration == reply.SourceGeneration && request.NetworkRevision == reply.NetworkRevision &&
		request.ProfileDigest == reply.ProfileDigest && request.CandidateID == reply.CandidateID && bytes.Equal(request.Nonce, reply.Nonce)
}

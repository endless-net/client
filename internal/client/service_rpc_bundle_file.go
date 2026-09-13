package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Includes base64 JSON and the Windows protected-state envelope overhead.
const rpcBundleFileMaxBytes = 2*rpcBundleStoreMaxBytes + (1 << 20)

type clientRPCBundleDiskRecord struct {
	Owner    string `json:"owner"`
	Profile  string `json:"profile"`
	Data     []byte `json:"data"`
	Metadata []byte `json:"metadata"`
}

// path must be inside the agent's private, ACL-protected state directory, never
// supplied by an RPC caller. The caller must hold the agent's single-writer lock.
// Windows uses the existing machine-DPAPI protection, which does not replace ACLs.
func openClientRPCBundleStore(path string, now func() time.Time) (*clientRPCBundleStore, error) {
	s := &clientRPCBundleStore{now: now, items: map[string]clientRPCBundleRecord{}}
	if path == "" {
		return nil, errors.New("bundle state path is required")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid bundle state path")
	}
	if err := validateRPCBundlePath(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("cannot inspect bundle state")
	}
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > rpcBundleFileMaxBytes ||
			!diagnosticsFilePermissionsSecure(path, info) {
			return nil, errors.New("unsafe bundle state file")
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return nil, errors.New("cannot open bundle state")
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, rpcBundleFileMaxBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(raw) > rpcBundleFileMaxBytes {
			return nil, errors.New("cannot read bundle state")
		}
		if err := s.restore(raw); err != nil {
			return nil, err
		}
	}
	s.persist = func(items map[string]clientRPCBundleRecord) error {
		if err := validateRPCBundlePath(path); err != nil {
			return err
		}
		ids := make([]string, 0, len(items))
		for id := range items {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		records := make([]clientRPCBundleDiskRecord, 0, len(ids))
		for _, id := range ids {
			item := items[id]
			metadata, err := proto.Marshal(item.metadata)
			if err != nil {
				return err
			}
			records = append(records, clientRPCBundleDiskRecord{Owner: item.owner, Profile: item.profile, Data: item.data, Metadata: metadata})
		}
		raw, err := json.Marshal(records)
		if err != nil {
			return err
		}
		raw, err = protectConfigState(raw)
		if err != nil {
			return err
		}
		if len(raw) > rpcBundleFileMaxBytes {
			return errors.New("bundle state exceeds storage limit")
		}
		return writeFileAtomicSecured(path, raw, 0o600, secureDiagnosticsBundleFile)
	}
	return s, nil
}

func (s *clientRPCBundleStore) restore(raw []byte) error {
	invalid := errors.New("invalid bundle state")
	raw, err := unprotectConfigState(raw)
	if err != nil || len(raw) > rpcBundleFileMaxBytes {
		return invalid
	}
	var records []clientRPCBundleDiskRecord
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&records) != nil || len(records) > 32 {
		return invalid
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return invalid
	}
	seen := map[string]bool{}
	total := 0
	for _, record := range records {
		metadata := &ipc.BundleResult{}
		if len(record.Metadata) > 4096 || proto.Unmarshal(record.Metadata, metadata) != nil {
			return invalid
		}
		digest := sha256.Sum256(record.Data)
		if record.Owner == "" || record.Profile == "" || len(record.Owner) > 4096 || len(record.Profile) > 4096 ||
			!validRPCUUID(metadata.BundleId) || seen[metadata.BundleId] || len(metadata.ProtoReflect().GetUnknown()) != 0 ||
			len(record.Data) == 0 || len(record.Data) > rpcBundleMaxBytes || metadata.SizeBytes != uint64(len(record.Data)) ||
			metadata.Sha256 != hex.EncodeToString(digest[:]) || metadata.CreatedAt.CheckValid() != nil || metadata.ExpiresAt.CheckValid() != nil {
			return invalid
		}
		lifetime := metadata.ExpiresAt.AsTime().Sub(metadata.CreatedAt.AsTime())
		if lifetime <= 0 || lifetime > 15*time.Minute {
			return invalid
		}
		seen[metadata.BundleId] = true
		total += len(record.Data)
		if total > rpcBundleStoreMaxBytes {
			return invalid
		}
		if !s.timeNow().Before(metadata.ExpiresAt.AsTime()) {
			s.dirty = true
			continue
		}
		s.items[metadata.BundleId] = clientRPCBundleRecord{owner: record.Owner, profile: record.Profile, data: record.Data, metadata: metadata}
		s.bytes += len(record.Data)
	}
	return nil
}

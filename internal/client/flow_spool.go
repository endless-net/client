package client

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"google.golang.org/protobuf/proto"
)

const maxFlowSpoolBytes = 1 << 20

var errInvalidFlowSpool = errors.New("invalid encrypted flow queue")

// flowSpool stores sealed windows only. AEAD binds the queue to the configured
// producer scope and credential without writing either value in plaintext.
type flowSpool struct {
	path  string
	aead  cipher.AEAD
	scope []byte
}
type flowSpoolBatch struct {
	ConsentVersion uint64
	ExpiresAt      time.Time
	Windows        [][]byte
}

func newFlowSpool(path, credential, scope string) (*flowSpool, error) {
	if path == "" || credential == "" || scope == "" {
		return nil, errInvalidFlowSpool
	}
	key := sha256.Sum256([]byte("endlessnet-flow-spool\x00" + credential))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &flowSpool{path: path, aead: aead, scope: []byte(scope)}, nil
}

func (s *flowSpool) discard() error {
	err := os.Remove(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *flowSpool) save(version uint64, expires time.Time, windows []*clientrpc.FlowWindow) error {
	if len(windows) == 0 {
		return s.discard()
	}
	if version == 0 || len(windows) > maxFlowWindows {
		return errInvalidFlowSpool
	}
	batch := flowSpoolBatch{ConsentVersion: version, ExpiresAt: expires}
	for _, window := range windows {
		if window == nil || window.GetWindowId() == "" || window.GetWindowStart() == nil || !window.GetWindowStart().IsValid() || window.GetWindowEnd() == nil || !window.GetWindowEnd().IsValid() {
			return errInvalidFlowSpool
		}
		raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(window)
		if err != nil {
			return err
		}
		batch.Windows = append(batch.Windows, raw)
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if len(raw) > maxFlowSpoolBytes-s.aead.NonceSize()-s.aead.Overhead() {
		return errInvalidFlowSpool
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(nonce, nonce, raw, s.scope)
	return WriteFileAtomic(s.path, sealed, 0600)
}

// load returns quarantined windows. The caller must obtain fresh consent and
// match its revision before importing or sending these windows. Expiry remains
// the original lease deadline; restarting cannot extend it.
func (s *flowSpool) load(now time.Time) (uint64, time.Time, []*clientrpc.FlowWindow, error) {
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, time.Time{}, nil, nil
	}
	if err != nil {
		return 0, time.Time{}, nil, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxFlowSpoolBytes+1))
	if err != nil {
		return 0, time.Time{}, nil, err
	}
	if len(raw) > maxFlowSpoolBytes || len(raw) < s.aead.NonceSize()+s.aead.Overhead() {
		return 0, time.Time{}, nil, errInvalidFlowSpool
	}
	plain, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], s.scope)
	if err != nil {
		return 0, time.Time{}, nil, errInvalidFlowSpool
	}
	var batch flowSpoolBatch
	if json.Unmarshal(plain, &batch) != nil || batch.ConsentVersion == 0 || len(batch.Windows) == 0 || len(batch.Windows) > maxFlowWindows {
		return 0, time.Time{}, nil, errInvalidFlowSpool
	}
	if !now.Before(batch.ExpiresAt) {
		// Close before deletion so expiry cleanup also works on Windows.
		if err := file.Close(); err != nil {
			return 0, time.Time{}, nil, err
		}
		return 0, time.Time{}, nil, s.discard()
	}
	if batch.ExpiresAt.After(now.Add(time.Minute)) {
		return 0, time.Time{}, nil, errInvalidFlowSpool
	}
	windows := make([]*clientrpc.FlowWindow, 0, len(batch.Windows))
	seen := make(map[string]bool)
	for _, raw := range batch.Windows {
		window := &clientrpc.FlowWindow{}
		if proto.Unmarshal(raw, window) != nil || window.GetWindowId() == "" || seen[window.GetWindowId()] || window.GetWindowStart() == nil || !window.GetWindowStart().IsValid() || window.GetWindowEnd() == nil || !window.GetWindowEnd().IsValid() || window.GetWindowEnd().AsTime().Before(window.GetWindowStart().AsTime()) || window.GetWindowEnd().AsTime().Sub(window.GetWindowStart().AsTime()) > 10*time.Second {
			return 0, time.Time{}, nil, errInvalidFlowSpool
		}
		seen[window.GetWindowId()] = true
		windows = append(windows, window)
	}
	return batch.ConsentVersion, batch.ExpiresAt, windows, nil
}

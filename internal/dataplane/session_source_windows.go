//go:build windows

package dataplane

import (
	"context"
	"errors"

	"github.com/endless-net/client/internal/connectivity"
)

type sourceEpochKey struct{}

func WithSourceEpoch(ctx context.Context, epoch uint64) context.Context {
	return context.WithValue(ctx, sourceEpochKey{}, epoch)
}
func (s *WindowsSession) sourceEpoch(ctx context.Context) uint64 {
	if epoch, ok := ctx.Value(sourceEpochKey{}).(uint64); ok {
		return epoch
	}
	epoch, _ := s.lease.SourceSignal()
	return epoch
}
func (s *WindowsSession) BlockSource() uint64 { return s.lease.BlockSource() }
func (s *WindowsSession) RevokeSource()       { s.lease.Revoke(); s.cancel() }

// A superseded apply can retain its acknowledged local resources, but cannot
// release the source gate. The serialized applier must apply the newer frame or
// offline policy next. This is not a fresh grant or an OS rollback/retry.
func (s *WindowsSession) resumeSource(epoch uint64) (bool, error) {
	err := s.lease.EndSourceTransition(epoch)
	if errors.Is(err, connectivity.ErrSourceChanged) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.lease.PausePackets(false)
	return true, nil
}

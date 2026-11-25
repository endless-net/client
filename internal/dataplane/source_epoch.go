package dataplane

import "context"

type sourceEpochKey struct{}

func WithSourceEpoch(ctx context.Context, epoch uint64) context.Context {
	return context.WithValue(ctx, sourceEpochKey{}, epoch)
}

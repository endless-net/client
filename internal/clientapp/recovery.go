package clientapp

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/endless-net/client/internal/connectivity"
)

func recoveryFailure(err error) bool {
	var kind connectivity.FailureKind
	if errors.As(err, &kind) {
		return kind == connectivity.FailurePath || kind == connectivity.FailureControl
	}
	return errors.Is(err, connectivity.ErrUnavailable) || errors.Is(err, connectivity.ErrEndpointUncertain)
}

func reconnectAfterFailure(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || recoveryFailure(err)
}

func reconnectBackoff(previous time.Duration) (base, delay time.Duration) {
	base = max(time.Second, min(15*time.Second, 2*previous))
	return base, base - rand.N(base/5)
}

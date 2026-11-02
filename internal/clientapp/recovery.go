package clientapp

import (
	"errors"

	"github.com/endless-net/client/internal/connectivity"
)

func recoveryFailure(err error) bool {
	var kind connectivity.FailureKind
	if errors.As(err, &kind) {
		return kind == connectivity.FailurePath || kind == connectivity.FailureControl
	}
	return errors.Is(err, connectivity.ErrUnavailable) || errors.Is(err, connectivity.ErrEndpointUncertain)
}

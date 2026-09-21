package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
)

// Bind the entire request lifetime, including response streaming, to the same
// immutable source lease. Current performs native observation elsewhere; this
// helper only links cancellation and must not recapture or replace the source.
func bindUnderlayHTTPRequest(request *http.Request, lease *underlayDNSLease) (*http.Request, func(), error) {
	if request == nil {
		return nil, nil, errors.New("underlay request requires a source lease")
	}
	if lease == nil {
		return request, func() {}, nil
	}
	lease.Start()
	if err := lease.Context().Err(); err != nil {
		return nil, nil, err
	}
	if err := request.Context().Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(lease.Context(), cancel)
	var once sync.Once
	cleanup := func() { once.Do(func() { stop(); cancel() }) }
	// Cancellation can race registration; do not dispatch a request after it
	// has become known that the captured source is no longer current.
	if err := lease.Context().Err(); err != nil {
		cleanup()
		return nil, nil, err
	}
	return request.Clone(ctx), cleanup, nil
}

type underlayHTTPBody struct {
	body       io.ReadCloser
	lease      *underlayDNSLease
	cleanup    func()
	finishOnce sync.Once
	closeOnce  sync.Once
	closeErr   error
}

func wrapUnderlayHTTPBody(body io.ReadCloser, lease *underlayDNSLease, cleanup func()) io.ReadCloser {
	if lease == nil {
		return body
	}
	if body == nil {
		if cleanup != nil {
			cleanup()
		}
		return nil
	}
	return &underlayHTTPBody{body: body, lease: lease, cleanup: cleanup}
}

func (b *underlayHTTPBody) finish() {
	b.finishOnce.Do(func() {
		if b.cleanup != nil {
			b.cleanup()
		}
	})
}

func (b *underlayHTTPBody) Read(p []byte) (int, error) {
	if b.lease == nil {
		b.finish()
		return 0, errors.New("underlay response requires a source lease")
	}
	if err := b.lease.Context().Err(); err != nil {
		_ = b.Close()
		return 0, err
	}
	n, err := b.body.Read(p)
	if revoked := b.lease.Context().Err(); revoked != nil {
		// Data obtained concurrently with revocation cannot be delivered as a
		// successful read, including readers returning both bytes and an error.
		clear(p[:n])
		_ = b.Close()
		return 0, revoked
	}
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *underlayHTTPBody) Close() error {
	b.closeOnce.Do(func() { b.finish(); b.closeErr = b.body.Close() })
	return b.closeErr
}

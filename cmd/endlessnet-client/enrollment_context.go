package main

import (
	"context"
	"io"
	"net/http"
	"sync"
)

// Preserve each HTTP request's deadline while also binding all workflow requests
// to the runtime lifetime. Do not cancel at response headers: decoding and crypto
// verification callers still need to consume the response body.
type enrollmentContextTransport struct {
	lifetime context.Context
	base     http.RoundTripper
	outcome  func(int, error)
}

func (t enrollmentContextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.lifetime.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.lifetime, cancel)
	finish := func() { stop(); cancel() }
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(req.Clone(ctx))
	if t.outcome != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.outcome(status, err)
	}
	if err != nil || response == nil || response.Body == nil {
		finish()
		return response, err
	}
	response.Body = &enrollmentContextBody{ReadCloser: response.Body, finish: finish}
	return response, nil
}

type enrollmentContextBody struct {
	io.ReadCloser
	finish func()
	once   sync.Once
}

func (b *enrollmentContextBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.finish)
	}
	return n, err
}

func (b *enrollmentContextBody) Close() error {
	defer b.once.Do(b.finish)
	return b.ReadCloser.Close()
}

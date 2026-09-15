package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
)

// Replacing the underlay must not restart a flow worker: worker shutdown purges
// its consent-bound queue. Wait for old response bodies after cancelling their
// requests so no old transport remains in use when replacement returns.
type rotatingControlClient struct {
	change sync.Mutex
	mu     sync.RWMutex
	client *http.Client
	ctx    context.Context
	cancel context.CancelFunc
}

func newRotatingControlClient(client *http.Client) *rotatingControlClient {
	c := &rotatingControlClient{}
	c.replace(client)
	return c
}

func (c *rotatingControlClient) replace(client *http.Client) {
	c.change.Lock()
	defer c.change.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
	c.client = client
	c.ctx, c.cancel = context.WithCancel(context.Background())
}

func (c *rotatingControlClient) Do(request *http.Request) (*http.Response, error) {
	c.mu.RLock()
	if c.client == nil {
		c.mu.RUnlock()
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, errors.New("control transport is closed")
	}
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(c.ctx, cancel)
	release := func() { stop(); cancel(); c.mu.RUnlock() }
	response, err := c.client.Do(request.Clone(ctx))
	if err != nil {
		release()
		return response, err
	}
	response.Body = &controlResponseBody{ReadCloser: response.Body, release: release}
	return response, nil
}

type controlResponseBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *controlResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

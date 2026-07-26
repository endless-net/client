package client

import (
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	localHTTPReadHeaderTimeout = 5 * time.Second
	localHTTPReadTimeout       = 15 * time.Second
	localHTTPIdleTimeout       = 120 * time.Second
	localHTTPMaxHeaderBytes    = 64 << 10
	localHTTPMaxPathBytes      = 2048
	localHTTPMaxQueryBytes     = 4096
	localHTTPMaxRequestURI     = 8192
)

// NewLocalHTTPServer applies the bounded request and timeout policy used by
// the client's loopback metrics and service endpoints.
func NewLocalHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           limitLocalRequestTarget(handler),
		ReadHeaderTimeout: localHTTPReadHeaderTimeout,
		ReadTimeout:       localHTTPReadTimeout,
		WriteTimeout:      0,
		IdleTimeout:       localHTTPIdleTimeout,
		MaxHeaderBytes:    localHTTPMaxHeaderBytes,
	}
}

func limitLocalRequestTarget(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > localHTTPMaxPathBytes ||
			len(r.URL.RawQuery) > localHTTPMaxQueryBytes ||
			len(r.RequestURI) > localHTTPMaxRequestURI {
			http.Error(w, "request target too long", http.StatusRequestURITooLong)
			return
		}
		if r.Body != nil {
			r.Body = &localTimeoutReportingBody{ReadCloser: r.Body, writer: w}
		}
		next.ServeHTTP(w, r)
	})
}

type localTimeoutReportingBody struct {
	io.ReadCloser
	writer http.ResponseWriter
	once   sync.Once
}

func (b *localTimeoutReportingBody) Read(buffer []byte) (int, error) {
	n, err := b.ReadCloser.Read(buffer)
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		b.once.Do(func() {
			b.writer.Header().Set("Connection", "close")
			http.Error(b.writer, "request body read timed out", http.StatusRequestTimeout)
		})
	}
	return n, err
}

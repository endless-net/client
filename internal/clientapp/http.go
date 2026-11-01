package clientapp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/endless-net/billing/api/public/v1/billingpublicapiconnect"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	"github.com/endless-net/identity/api/v1/identityapiconnect"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type observedHTTP struct {
	base    http.RoundTripper
	signals *Signals
}

func (s *Signals) HTTPTransport(base http.RoundTripper) (http.RoundTripper, error) {
	if !s.valid() || base == nil {
		return nil, ErrSignals
	}
	// Preserve the existing native control adapters' rejection of workload
	// credentials and disabled server-certificate verification.
	if transport, ok := base.(*http.Transport); ok && transport.TLSClientConfig != nil {
		c := transport.TLSClientConfig
		if c.InsecureSkipVerify || len(c.Certificates) != 0 || c.GetClientCertificate != nil {
			return nil, ErrSignals
		}
	}
	if transport, ok := base.(*http.Transport); ok {
		base = transport.Clone()
	}
	return &observedHTTP{base: base, signals: s}, nil
}

func httpOperation(r *http.Request) string {
	if r.Method == http.MethodGet && r.URL.Path == "/server-key" {
		return "server_key"
	}
	if r.Method != http.MethodPost {
		return "unsupported"
	}
	switch r.URL.Path {
	case "/nodes/register":
		return "register_node"
	case identityapiconnect.NativeLoginServiceStartNativeLoginProcedure:
		return "start_native_login"
	case identityapiconnect.NativeLoginServiceGetNativeLoginProcedure:
		return "get_native_login"
	case identityapiconnect.NativeLoginServiceCancelNativeLoginProcedure:
		return "cancel_native_login"
	case billingpublicapiconnect.BillingUserServiceListAccountsProcedure:
		return "list_accounts"
	case publicqueryv1connect.CoordinatorUserServiceListNetworksProcedure:
		return "list_networks"
	default:
		return "unsupported"
	}
}

func httpResult(ctx context.Context, status int, err error) (string, string) {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return "cancelled", "cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "deadline", "deadline"
		}
		return "network_error", "failure"
	}
	switch {
	case status >= 100 && status < 200:
		return "1xx", "success"
	case status >= 200 && status < 300:
		return "2xx", "success"
	case status >= 300 && status < 400:
		return "3xx", "failure"
	case status == 401 || status == 403:
		return "4xx", "denied"
	case status == 429:
		return "4xx", "limited"
	case status >= 400 && status < 500:
		return "4xx", "failure"
	case status >= 500 && status < 600:
		return "5xx", "failure"
	default:
		return "other", "failure"
	}
}

func (t *observedHTTP) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, ErrSignals
	}
	ctx, complete := t.signals.request(request.Context(), "http", httpOperation(request), trace.SpanKindClient)
	outgoing := request.Clone(ctx)
	if outgoing.Header == nil {
		outgoing.Header = make(http.Header)
	}
	for _, key := range []string{"traceparent", "tracestate", "baggage"} {
		outgoing.Header.Del(key)
	}
	(propagation.TraceContext{}).Inject(ctx, propagation.HeaderCarrier(outgoing.Header))
	response, err := t.base.RoundTrip(outgoing)
	if err != nil {
		code, outcome := httpResult(ctx, 0, err)
		complete(code, outcome)
		return response, err
	}
	if response == nil {
		code, outcome := httpResult(ctx, 0, ErrSignals)
		complete(code, outcome)
		return nil, ErrSignals
	}
	status := response.StatusCode
	if response.Body == nil {
		code, outcome := httpResult(ctx, status, nil)
		complete(code, outcome)
	} else {
		response.Body = &observedBody{ReadCloser: response.Body, finish: func(err error) { code, outcome := httpResult(ctx, status, err); complete(code, outcome) }}
	}
	return response, nil
}

func (t *observedHTTP) CloseIdleConnections() {
	if idle, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		idle.CloseIdleConnections()
	}
}

type observedBody struct {
	io.ReadCloser
	once   sync.Once
	finish func(error)
}

func (b *observedBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if err != nil {
		b.once.Do(func() {
			if errors.Is(err, io.EOF) {
				b.finish(nil)
			} else {
				b.finish(err)
			}
		})
	}
	return n, err
}
func (b *observedBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { b.finish(err) })
	return err
}

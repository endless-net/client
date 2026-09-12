// Package testserver provides strict, scriptable Client v0 RPC fixtures.
// It does not emulate backend authorization or claim runtime conformance.
package testserver

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	pb "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/clientipc/v0/clientipcconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Step describes exactly one accepted invocation. Request and responses are
// cloned on enqueue. Release allows a test to delay completion without sleeps.
// Err can model a typed failure, including after the last stream event.
type Step struct {
	Method    string
	Request   proto.Message
	Responses []proto.Message
	Err       error
	Release   <-chan struct{}
	// HoldOpen keeps a stream active after its scripted responses until cancellation.
	HoldOpen bool
}

// Server implements every v0 RPC. Calls consume per-method FIFO expectations;
// unexpected calls fail closed and remain visible through Verify.
// Request contents are never included in error messages or call records.
type Server struct {
	mu       sync.Mutex
	steps    map[string][]Step
	failures []string
	active   int
}

func New() *Server { return &Server{steps: make(map[string][]Step)} }

// Expect validates the script's protobuf types before any RPC is served.
// An empty stream and malformed field values are allowed for negative tests.
func (s *Server) Expect(step Step) error {
	method := pb.File_client_v0_service_proto.Services().ByName("ClientService").Methods().ByName(protoreflect.Name(step.Method))
	if method == nil {
		return errors.New("unknown Client v0 method")
	}
	if step.HoldOpen && (!method.IsStreamingServer() || step.Err != nil) {
		return errors.New("hold_open requires a stream without a terminal error")
	}
	if step.Request == nil || !step.Request.ProtoReflect().IsValid() || step.Request.ProtoReflect().Descriptor().FullName() != method.Input().FullName() {
		return errors.New("script request type does not match method")
	}
	if !method.IsStreamingServer() && ((step.Err == nil && len(step.Responses) != 1) || (step.Err != nil && len(step.Responses) != 0)) {
		return errors.New("unary step requires exactly one response or an error")
	}
	step.Request = proto.Clone(step.Request)
	responses := make([]proto.Message, len(step.Responses))
	for i, response := range step.Responses {
		if response == nil || !response.ProtoReflect().IsValid() || response.ProtoReflect().Descriptor().FullName() != method.Output().FullName() {
			return errors.New("script response type does not match method")
		}
		responses[i] = proto.Clone(response)
	}
	step.Responses = responses
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps[step.Method] = append(s.steps[step.Method], step)
	return nil
}

func (s *Server) take(method string, request proto.Message) (Step, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.steps[method]
	if len(queue) == 0 {
		s.failures = append(s.failures, method+": unexpected invocation")
		return Step{}, rpc.Error(connect.CodeUnimplemented, pb.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	step := queue[0]
	if !proto.Equal(step.Request, request) {
		s.failures = append(s.failures, method+": request mismatch")
		return Step{}, rpc.Error(connect.CodeInvalidArgument, pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	s.steps[method] = queue[1:]
	s.active++
	return step, nil
}

func (s *Server) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
}

func wait(ctx context.Context, release <-chan struct{}) error {
	if release == nil {
		return ctx.Err()
	}
	select {
	case <-release:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) invoke(ctx context.Context, method string, request proto.Message) (proto.Message, error) {
	step, err := s.take(method, request)
	if err != nil {
		return nil, err
	}
	defer s.finish()
	if err := wait(ctx, step.Release); err != nil {
		return nil, err
	}
	if step.Err != nil {
		return nil, step.Err
	}
	return step.Responses[0], nil
}

// Verify reports unexpected calls, unmatched expectations and in-flight calls.
// Only method names and counts are reported, never enrollment material.
func (s *Server) Verify() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failures) > 0 {
		return errors.New(s.failures[0])
	}
	if s.active != 0 {
		return errors.New("script has in-flight calls")
	}
	for _, queue := range s.steps {
		if len(queue) > 0 {
			return errors.New("script has unconsumed expectations")
		}
	}
	return nil
}

// Handler uses the producer guard. Authorization must be supplied by the test
// host using authenticated transport context; nil rejects every request.
// No RPC can change the test script or supply a claimed role.
func (s *Server) Handler(authorize func(context.Context, pb.Access, string) error) http.Handler {
	_, handler := clientipcconnect.NewClientServiceHandler(s,
		connect.WithInterceptors(rpc.Guard{Authorize: authorize}),
		connect.WithReadMaxBytes(rpc.MaxRequestBytes),
		connect.WithSendMaxBytes(rpc.MaxResponseBytes))
	return handler
}

var _ clientipcconnect.ClientServiceHandler = (*Server)(nil)

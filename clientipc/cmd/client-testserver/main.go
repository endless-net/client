// client-testserver serves synthetic Client v0 scripts on an isolated local endpoint.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	"github.com/endless-net/client/clientipc/testserver"
	pb "github.com/endless-net/client/clientipc/v0"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// The parent sends "verify" on stdin after closing all consumer channels.
// EOF/parent loss also stops the server but cannot claim successful completion.
func run(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("client-testserver", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scriptPath := flags.String("script", "", "synthetic scenario JSON file")
	endpoint := flags.String("endpoint", "", "unique local test pipe/socket")
	accessName := flags.String("access", "observer", "simulated role for authenticated local peers")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("invalid testserver arguments")
	}
	if *scriptPath == "" || *endpoint == "" {
		return errors.New("explicit script and isolated endpoint are required")
	}
	if *endpoint == local.DefaultWindowsPipe || *endpoint == local.DefaultUnixSocket || *endpoint == local.DefaultDarwinSocket {
		return errors.New("production endpoint is forbidden")
	}
	access, ok := map[string]pb.Access{"observer": pb.Access_ACCESS_OBSERVER, "owner": pb.Access_ACCESS_OWNER, "administrator": pb.Access_ACCESS_ADMINISTRATOR}[*accessName]
	if !ok {
		return errors.New("invalid simulated role")
	}
	file, err := os.Open(*scriptPath)
	if err != nil {
		return errors.New("cannot open test script")
	}
	script, err := testserver.Load(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	listener, err := local.Listen(*endpoint)
	if err != nil {
		return errors.New("cannot listen on test endpoint")
	}
	defer func() { _ = listener.Close() }()
	handler := script.Handler(func(ctx context.Context, required pb.Access, _ string) error {
		if _, ok := local.PeerFromContext(ctx); !ok {
			return rpc.Error(connect.CodeUnauthenticated, pb.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
		}
		if access < required {
			code := pb.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			if required == pb.Access_ACCESS_ADMINISTRATOR {
				code = pb.ErrorCode_ERROR_CODE_ADMINISTRATOR_REQUIRED
			}
			return rpc.Error(connect.CodePermissionDenied, code)
		}
		return nil
	})
	server := local.NewServer(handler)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() { _ = server.Close() }()
	if err := json.NewEncoder(output).Encode(map[string]string{"event": "ready", "contract_sha256": rpc.Digest()}); err != nil {
		return errors.New("cannot report readiness")
	}
	commands := make(chan string, 1)
	stopCommands := make(chan struct{})
	defer close(stopCommands)
	go func() {
		defer close(commands)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 64), 1024)
		for scanner.Scan() {
			select {
			case commands <- scanner.Text():
			case <-stopCommands:
				return
			}
		}
	}()
	for {
		select {
		case err := <-done:
			if err == nil || err == http.ErrServerClosed {
				return errors.New("testserver stopped before verification")
			}
			return errors.New("testserver transport failed")
		case command := <-commands:
			if strings.HasPrefix(command, "release ") {
				if err := script.ReleaseGate(strings.TrimPrefix(command, "release ")); err != nil {
					return err
				}
				if err := json.NewEncoder(output).Encode(map[string]string{"event": "released"}); err != nil {
					return errors.New("cannot report gate release")
				}
				continue
			}
			if command != "verify" {
				return errors.New("parent ended without explicit verification")
			}
			// Parent-side channel termination and server-side handler completion
			// are asynchronous. Wait for completion, not an arbitrary sleep, while
			// retaining failures for leaked streams and unconsumed expectations.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := script.WaitIdle(ctx); err != nil {
				return errors.New("testserver calls did not finish before verification")
			}
			if err := script.Verify(); err != nil {
				return err
			}
			if err := server.Close(); err != nil {
				return errors.New("cannot stop testserver")
			}
			if err := <-done; err != http.ErrServerClosed {
				return errors.New("testserver transport failed")
			}
			return json.NewEncoder(output).Encode(map[string]string{"event": "verified"})
		}
	}
}

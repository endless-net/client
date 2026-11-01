//go:build windows && integration

package localipc

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"golang.org/x/sys/windows"
)

func TestNamedPipeOSAuthentication(t *testing.T) {
	if testing.Short() {
		t.Skip("real Windows pipe acceptance is separate from unit CI")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
	config := PipeConfig{InstanceID: id, ServerSID: sid, ContractDigest: [32]byte{1}, HandshakeTimeout: 2 * time.Second}
	listener, err := Listen(config, []string{sid})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint("invalid-http2-preface=", mismatch), func(t *testing.T) {
			result := make(chan error, 1)
			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					result <- acceptErr
					return
				}
				defer func() { _ = conn.Close() }()
				creds, credsErr := Credentials(config)
				if credsErr != nil {
					result <- credsErr
					return
				}
				_, info, handshakeErr := creds.ServerHandshake(conn)
				if handshakeErr == nil && info.(AuthInfo).principal.SID != sid {
					handshakeErr = fmt.Errorf("unexpected OS caller")
				}
				result <- handshakeErr
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			clientConfig := config
			conn, err := Dial(ctx, clientConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := conn.Close(); err != nil && err != net.ErrClosed {
					t.Error(err)
				}
			}()
			creds, err := Credentials(clientConfig)
			if err != nil {
				t.Fatal(err)
			}
			_, _, clientErr := creds.ClientHandshake(ctx, "", conn)
			if clientErr != nil {
				t.Fatal(clientErr)
			}
			preface := http2.ClientPreface
			if mismatch {
				preface = strings.Repeat("x", len(preface))
			}
			_, clientErr = io.WriteString(conn, preface)
			select {
			case serverErr := <-result:
				if mismatch && serverErr == nil {
					t.Fatal("invalid HTTP2 preface accepted")
				}
				if !mismatch && (serverErr != nil || clientErr != nil) {
					t.Fatalf("server=%v client=%v", serverErr, clientErr)
				}
			case <-ctx.Done():
				t.Fatal("handshake did not finish")
			}
		})
	}
}

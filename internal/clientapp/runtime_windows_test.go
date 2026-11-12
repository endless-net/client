//go:build windows

package clientapp

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"errors"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
)

func runtimeMetadata(t *testing.T) (*clientv0.BuildIdentity, [32]byte) {
	t.Helper()
	// Published Client API0.2.0 descriptor, separately resolved/rebuilt with Buf.
	raw, err := hex.DecodeString("cf89bccd1774dbb4d720945832be302fe7d15be886892b48d218bc1cabbfebd3")
	if err != nil {
		t.Fatal(err)
	}
	var digest [32]byte
	copy(digest[:], raw)
	return &clientv0.BuildIdentity{Version: "fixture", Commit: "fixture", Platform: clientv0.Platform_PLATFORM_WINDOWS, Architecture: runtime.GOARCH}, digest
}

func TestWindowsAssemblyDoesNotAcquireStateOrPipeDuringConstruction(t *testing.T) {
	e := configEnvironment()
	directory := filepath.Join(t.TempDir(), "not-created")
	e["ENDLESSNET_CLIENT_STATE_DIRECTORY"] = directory
	instancePath := filepath.Join(filepath.Dir(directory), "runtime-instance")
	e["ENDLESSNET_CLIENT_RUNTIME_INSTANCE_FILE"] = instancePath
	c, err := ParseWindows(e)
	if err != nil {
		t.Fatal(err)
	}
	build, digest := runtimeMetadata(t)
	var logs bytes.Buffer
	r, err := NewWindows(c, build, digest, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(instancePath); !os.IsNotExist(err) {
		t.Fatal("constructor published an instance before service startup")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := r.telemetry.stop(ctx); err != nil {
			t.Error(err)
		}
	})
	if r.state.gate.backend != nil || r.ipc.listener != nil || r.workers.Healthy() || logs.Len() != 0 {
		t.Fatal("constructor started resource or logging")
	}
	if r.connection == nil || r.connection.factory == nil || r.connection.config.RuntimeID != r.pipe.InstanceID || r.connection.running || r.workers.connection == nil || r.controller == nil {
		t.Fatal("connection backend missing or started during construction")
	}
	if r.bootstrap == nil {
		t.Fatal("runtime missing strict bootstrap source")
	}
	if announcement, err := r.bootstrap.Fetch(t.Context(), c.ControlOrigin); !errors.Is(err, control.ErrBootstrap) || announcement.VerifiedAt(time.Now()) {
		t.Fatal("actual NewWindows accepted missing independently embedded root", err)
	}
	sample, err := r.connection.config.Clock.Sample()
	if err != nil || sample.Epoch != r.pipe.InstanceID {
		t.Fatal("runtime connection clock has a different authority epoch", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("constructor created or opened state", err)
	}
	if r.PipeConfig().InstanceID == c.InstallationID || !validConfigID(r.PipeConfig().InstanceID) || r.PipeConfig().ContractDigest != digest {
		t.Fatal("process instance/contract identity invalid")
	}
	if _, err := NewWindows(c, nil, digest, &logs); err != ErrConfig {
		t.Fatal("missing build identity accepted")
	}
	if _, err := NewWindows(c, build, [32]byte{}, &logs); err != ErrConfig {
		t.Fatal("missing contract digest accepted")
	}
	c.AllowedSIDs[0] = "forged"
	if r.config.AllowedSIDs[0] == "forged" || r.ipc.allowed[0] == "forged" {
		t.Fatal("caller mutated IPC admission")
	}

}

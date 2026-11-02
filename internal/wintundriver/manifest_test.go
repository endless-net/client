package wintundriver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

type failedRead struct{}

func (failedRead) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestBinaryVerificationRejectsTruncationMutationTrailingBytesAndReadErrors(t *testing.T) {
	data := []byte("pinned binary fixture")
	digest := sha256.Sum256(data)
	pin := binaryPin{Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
	if err := verify(bytes.NewReader(data), pin); err != nil {
		t.Fatal(err)
	}
	for _, input := range []io.Reader{nil, bytes.NewReader(data[:len(data)-1]), bytes.NewReader(append(append([]byte(nil), data...), 0)), bytes.NewReader(bytes.Repeat([]byte{0}, len(data))), failedRead{}} {
		if err := verify(input, pin); !errors.Is(err, ErrDriver) {
			t.Fatal("unverified bytes accepted", err)
		}
	}
	m, err := bundled()
	if err != nil || m.Version != "0.14.1" || m.Files["wintun.dll"].Size != 427552 || m.Files["WINTUN-LICENSE.txt"].Size <= 0 {
		t.Fatal("missing embedded release pin", err)
	}
}

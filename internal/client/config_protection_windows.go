//go:build windows

package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsConfigProtectionFormat   = "endlessnet-client-state-dpapi-v1"
	windowsConfigProtectionProvider = "windows-dpapi-machine"
)

type windowsProtectedConfigState struct {
	Format     string `json:"format"`
	Protection string `json:"protection"`
	Ciphertext string `json:"ciphertext"`
}

func protectConfigState(raw []byte) ([]byte, error) {
	protected, err := cryptProtectMachine(raw)
	if err != nil {
		return nil, err
	}
	envelope := windowsProtectedConfigState{
		Format:     windowsConfigProtectionFormat,
		Protection: windowsConfigProtectionProvider,
		Ciphertext: base64.StdEncoding.EncodeToString(protected),
	}
	return json.MarshalIndent(envelope, "", "  ")
}

func unprotectConfigState(raw []byte) ([]byte, error) {
	var envelope windowsProtectedConfigState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode protected client state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("protected client state contains trailing JSON")
		}
		return nil, fmt.Errorf("decode protected client state: %w", err)
	}
	if envelope.Format != windowsConfigProtectionFormat {
		return nil, fmt.Errorf("unsupported protected state format %q", envelope.Format)
	}
	if envelope.Protection != windowsConfigProtectionProvider {
		return nil, fmt.Errorf("unsupported protected state provider %q", envelope.Protection)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode protected state: %w", err)
	}
	return cryptUnprotectMachine(ciphertext)
}

func cryptProtectMachine(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("client state is empty")
	}
	in := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var out windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_LOCAL_MACHINE | windows.CRYPTPROTECT_UI_FORBIDDEN)
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, flags, &out); err != nil {
		return nil, fmt.Errorf("windows DPAPI protect: %w", err)
	}
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data))) }()
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

func cryptUnprotectMachine(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("protected client state is empty")
	}
	in := windows.DataBlob{Size: uint32(len(ciphertext)), Data: &ciphertext[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, fmt.Errorf("windows DPAPI unprotect: %w", err)
	}
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data))) }()
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

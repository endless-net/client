package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCryptoOrigin(t *testing.T) {
	origin := map[string]string{"VCS": "git", "Hash": strings.Repeat("a", 40), "Ref": "refs/tags/v0.1.0", "URL": "https://github.com/example/crypto"}
	for _, tc := range []struct {
		name, version string
		reply         json.RawMessage
		valid         bool
	}{
		{"cold", "v0.1.0", nil, true},
		{"null", "v0.1.0", json.RawMessage("null"), false},
		{"wrong version", "v0.2.0", nil, false},
		{"conflicting reply", "v0.1.0", json.RawMessage(`{"VCS":"git","Hash":"wrong"}`), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "module.info")
			data, err := json.Marshal(map[string]any{"Version": tc.version, "Origin": origin})
			if err != nil {
				t.Fatal(err)
			}
			put(t, file, string(data))
			d := downloadInfo{Info: file, Version: "v0.1.0", Origin: tc.reply}
			found, err := d.verifiedOrigin()
			if (err == nil) != tc.valid || tc.valid && !maps.Equal(origin, found) {
				t.Fatalf("%v %v", found, err)
			}
		})
	}
}

func TestCryptoModuleArchive(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	hash := sha256.New()
	pin := cryptoPin{Path: "example.org/crypto", Version: "v0.1.0"}
	for _, name := range []string{".github/test.yml", "LICENSE", "README.md", "device/noise.go"} {
		full := pin.Path + "@" + pin.Version + "/" + name
		file, err := archive.Create(full)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
		_, _ = fmt.Fprintf(hash, "%x  %s\n", sha256.Sum256([]byte(name)), full)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	pin.Sum = "h1:" + base64.StdEncoding.EncodeToString(hash.Sum(nil))
	files, err := upstreamZIP(buffer.Bytes(), pin)
	if err != nil || len(files) != 2 || string(files["LICENSE"]) != "LICENSE" || string(files["device/noise.go"]) != "device/noise.go" {
		t.Fatalf("upstreamZIP: %v %v", files, err)
	}
	pin.Sum = "h1:wrong"
	if _, err := upstreamZIP(buffer.Bytes(), pin); err == nil {
		t.Fatal("accepted incorrect module checksum")
	}
}

func TestCryptoWorkerBytes(t *testing.T) {
	a, err := worker([]byte("package device\nfunc (d *Device) RoutineEncryption() {\n encrypt()\n}\n// next function\n"), "RoutineEncryption")
	if err != nil {
		t.Fatal(err)
	}
	b, err := worker([]byte("package device\n// queue metadata\nfunc (d *Device) RoutineEncryption() {\n encrypt()\n}\n// modified code outside worker\n"), "RoutineEncryption")
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("outside changes affect worker: %v", err)
	}
	c, err := worker([]byte("package device\nfunc (d *Device) RoutineEncryption() {\n different()\n}\n"), "RoutineEncryption")
	if err != nil || bytes.Equal(a, c) {
		t.Fatalf("changed worker appears unchanged: %v", err)
	}
}

func TestWintunArchive(t *testing.T) {
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	manifest := driverManifest{Files: make(map[string]driverFile)}
	for name, entry := range map[string]string{"wintun.dll": "wintun/bin/amd64/wintun.dll", "WINTUN-LICENSE.txt": "wintun/LICENSE.txt"} {
		file, err := w.Create(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(name)); err != nil {
			t.Fatal(err)
		}
		manifest.Files[name] = driverFile{Entry: entry, Size: int64(len(name)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(name)))}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := extractDriver(buffer.Bytes(), manifest)
	if err != nil || len(entries) != 2 {
		t.Fatalf("extract Wintun: %v %v", entries, err)
	}
	file := manifest.Files["wintun.dll"]
	file.SHA256 = strings.Repeat("0", 64)
	manifest.Files["wintun.dll"] = file
	if _, err := extractDriver(buffer.Bytes(), manifest); err == nil {
		t.Fatal("accepted changed Wintun bytes")
	}
}

func TestCryptoInventory(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "LICENSE"), "fixture")
	expected := map[string]string{"LICENSE": fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))}
	if err := verifyCryptoFiles(root, expected); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "unexpected"), "content")
	if err := verifyCryptoFiles(root, expected); err == nil {
		t.Fatal("accepted undeclared crypto source")
	}
	if err := os.Remove(filepath.Join(root, "unexpected")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "LICENSE"), "changed")
	if err := verifyCryptoFiles(root, expected); err == nil {
		t.Fatal("accepted changed source")
	}
}

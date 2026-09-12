package clientipc_test

import (
	"bytes"
	"os"
	"testing"
)

func TestDartPackageCarriesRepositoryLicense(t *testing.T) {
	repository, err := os.ReadFile("../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	packaged, err := os.ReadFile("../packages/client_api/LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	normalize := func(data []byte) []byte { return bytes.TrimSpace(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))) }
	if !bytes.Equal(normalize(repository), normalize(packaged)) {
		t.Fatal("Dart SDK must carry the unmodified repository license")
	}
}

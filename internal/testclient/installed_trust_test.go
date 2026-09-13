package testclient

import "testing"

func TestInstalledTrustRejectsLocalAndPersistentRunners(t *testing.T) {
	for _, input := range [][2]string{{"", ""}, {"true", "self-hosted"}, {"true", ""}, {"false", "github-hosted"}, {"TRUE", "github-hosted"}, {"", "github-hosted"}} {
		if installedTrustRunner(input[0], input[1]) == nil {
			t.Fatal("non-disposable execution admitted to machine trust fixture")
		}
	}
	if err := installedTrustRunner("true", "github-hosted"); err != nil {
		t.Fatal("disposable hosted execution rejected")
	}
}

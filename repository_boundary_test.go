package clientrepo_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientRepositoryHasNoBackendInternalDependency(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".private-modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{
			`"endlessnet/` + `internal/`,
			`"github.com/endless-net/` + `internal/`,
		} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("%s imports backend internals via %q", filepath.ToSlash(path), forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientRepositoryPinsProducerContractWithoutReplace(t *testing.T) {
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	text := string(goMod)
	if !strings.Contains(text, "github.com/endless-net/client-api/clientapi v") {
		t.Fatal("go.mod does not pin the producer-owned clientapi module")
	}
	if strings.Contains(text, "replace github.com/endless-net/client-api/clientapi") {
		t.Fatal("go.mod uses a local clientapi replacement")
	}

	workflow, err := os.ReadFile(".github/workflows/publish-client-core.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(workflow)
	for _, expected := range []string{
		"endless-net/client",
		"endlessnet-client_linux_amd64.manifest.json.sha256",
	} {
		if !strings.Contains(workflowText, expected) {
			t.Fatalf("client release workflow does not contain %q", expected)
		}
	}
	for _, forbidden := range []string{
		"CLIENT_UI_DISPATCH_TOKEN",
		"client-core-published",
		"endlessnet-client-ui/dispatches",
	} {
		if strings.Contains(workflowText, forbidden) {
			t.Fatalf("client release workflow retains UI release dispatch coupling %q", forbidden)
		}
	}
}

func TestPublicModuleCIUsesNoPrivateRepositoryCredentials(t *testing.T) {
	if _, err := os.Stat(".github/actions/setup-private-go-modules/action.yml"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private module setup action still exists: %v", err)
	}
	for _, path := range []string{
		".github/workflows/test.yml",
		".github/workflows/publish-client-core.yml",
		".github/workflows/publish-apt.yml",
	} {
		workflow, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		workflowText := string(workflow)
		for _, forbidden := range []string{
			"setup-private-go-modules",
			"BACKEND_REPO_DEPLOY_KEY",
			"RELAY_REPO_DEPLOY_KEY",
			"STUN_REPO_DEPLOY_KEY",
			"self-hosted",
		} {
			if strings.Contains(workflowText, forbidden) {
				t.Fatalf("%s still contains private CI coupling %q", path, forbidden)
			}
		}
	}
}

func TestAPTReleaseUsesRepositoryScopedDeployKey(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/publish-apt.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(workflow)
	for _, expected := range []string{
		"APT_REPO_DEPLOY_KEY: ${{ secrets.APT_REPO_DEPLOY_KEY }}",
		"ssh-key: ${{ secrets.APT_REPO_DEPLOY_KEY }}",
	} {
		if !strings.Contains(workflowText, expected) {
			t.Fatalf("APT release workflow does not contain %q", expected)
		}
	}
	if strings.Contains(workflowText, "APT_REPO_TOKEN") {
		t.Fatal("APT release workflow still requests a repository-wide token")
	}
}

func TestAPTPackageMigratesLegacyStateBeforeRestart(t *testing.T) {
	script, err := os.ReadFile("scripts/build-deb.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, expected := range []string{
		`cat > "$control_dir/preinst"`,
		"systemctl stop $package_name.service",
		"$package_name state migrate",
		`--backup "\$state_path.pre-migration-v2.bak"`,
		"systemctl start $package_name.service",
		`chmod 0755 "$control_dir/preinst"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("APT package lifecycle does not contain %q", expected)
		}
	}
	if strings.Index(text, "$package_name state migrate") > strings.Index(text, "systemctl start $package_name.service") {
		t.Fatal("APT package restarts the client before migrating legacy state")
	}
}

func TestWorkflowRunnerSelectors(t *testing.T) {
	workflows := map[string]string{
		".github/workflows/test.yml":                "runs-on: ubuntu-latest",
		".github/workflows/publish-client-core.yml": "runs-on: ubuntu-latest",
		".github/workflows/publish-apt.yml":         "runs-on: ubuntu-latest",
	}
	for path, expected := range workflows {
		workflow, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(workflow), expected) {
			t.Fatalf("%s does not contain the required runner selector %q", path, expected)
		}
	}
	testWorkflow, err := os.ReadFile(".github/workflows/test.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflowText := string(testWorkflow)
	for _, expected := range []string{
		"verify-windows:",
		"runs-on: windows-2025",
		"verify-macos:",
		"runs-on: macos-15",
		"needs: [verify-linux, verify-windows, verify-macos]",
	} {
		if !strings.Contains(workflowText, expected) {
			t.Fatalf("Windows verification does not contain %q", expected)
		}
	}
}

func TestOnlyIPCContractIsPublic(t *testing.T) {
	if _, err := os.Stat("ipc/v2/contract.go"); err != nil {
		t.Fatalf("public IPC contract is missing: %v", err)
	}
	if _, err := os.Stat("ipc/v1/contract.go"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("superseded IPC v1 contract still exists")
	}
	for _, path := range []string{
		"config/config.go",
		"diagnostics/v1/types.go",
		"identity/identity.go",
		"stunclient/protocol.go",
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("removed compatibility package %s still exists", path)
		}
	}
}

func TestClientCoreReleasePublishesCurrentIPCContract(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/publish-client-core.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	if !strings.Contains(text, `contract_name="client-ipc-v2.openapi.yaml"`) ||
		!strings.Contains(text, `cp docs/client-ipc-v2.openapi.yaml "$output_dir/$contract_name"`) ||
		!strings.Contains(text, `"ipc_version": "v2"`) {
		t.Fatalf("client-core release workflow does not publish the IPC v2 contract")
	}
	if strings.Contains(text, "client-ipc-v1.openapi.yaml") {
		t.Fatalf("client-core release workflow still references the superseded IPC v1 contract")
	}
}

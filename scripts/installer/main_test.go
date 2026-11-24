package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguration(t *testing.T) {
	defaults := map[string]string{prefix + "CONNECTION_LISTEN_PORT": "51820"}
	makeConfig := func(server, endpoint string) (map[string]string, error) {
		return configuration(defaults, server, endpoint, "S-1-5-21-1001", "state")
	}
	first, err := makeConfig("https://endlessnet.ru", "192.0.2.1:51820")
	if err != nil {
		t.Fatal(err)
	}
	second, err := makeConfig("https://endlessnet.ru/", "192.0.2.1:51820")
	if err != nil {
		t.Fatal(err)
	}
	if first[prefix+"CONTROL_ORIGIN"] != "https://api.endlessnet.ru" ||
		first[prefix+"APPROVAL_BASE"] != "https://admin.endlessnet.ru/" ||
		first[prefix+"DEFAULT_PROFILE_ID"] != second[prefix+"DEFAULT_PROFILE_ID"] ||
		first[prefix+"INSTALLATION_ID"] == second[prefix+"INSTALLATION_ID"] {
		t.Fatal("unexpected installation identity")
	}
	for _, server := range []string{"http://endlessnet.ru", "https://user@endlessnet.ru", "https://endlessnet.ru/path", "https://endlessnet.ru?key=x"} {
		if _, err := makeConfig(server, "192.0.2.1:51820"); err == nil {
			t.Fatalf("accepted invalid origin %q", server)
		}
	}
	for _, ip := range []string{"127.0.0.1:51820", "0.0.0.0:51820", "169.254.1.1:51820", "192.0.2.1:0", "192.0.2.1:51821"} {
		if _, err := makeConfig("https://endlessnet.ru", ip); err == nil {
			t.Fatalf("accepted invalid endpoint %q", ip)
		}
	}
	defaults[prefix+"INSTALLATION_ID"] = "fixed"
	if _, err := makeConfig("https://endlessnet.ru", "192.0.2.1:51820"); err == nil {
		t.Fatal("accepted machine identity from defaults")
	}
}

type node struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []node     `xml:",any"`
}

func (n node) attr(key string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}
func (n node) find(tag, key, value string) *node {
	if n.XMLName.Local == tag && (key == "" || n.attr(key) == value) {
		return &n
	}
	for _, child := range n.Children {
		if match := child.find(tag, key, value); match != nil {
			return match
		}
	}
	return nil
}

func TestWiXTransactions(t *testing.T) {
	for _, ui := range []bool{false, true} {
		name := "standalone"
		if ui {
			name = "combined"
		}
		t.Run(name, func(t *testing.T) {
			tmp := t.TempDir()
			o := renderOptions{bundle: filepath.Join(tmp, "client"), output: filepath.Join(tmp, "out"),
				actions: filepath.Join(tmp, "actions.dll"), version: "v0.9.0",
				service: map[string]string{prefix + "CONNECTION_LISTEN_PORT": "51820", prefix + "DEFAULT_PROFILE_ID": "profile",
					prefix + "CONTROL_ORIGIN": "https://example.org", prefix + "LOG_LEVEL": "literal[$X]"}}
			if err := os.Mkdir(o.bundle, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, f := range payload {
				putFile(t, filepath.Join(o.bundle, f), []byte("payload"))
			}
			putFile(t, o.actions, []byte("actions"))
			file, code := "EndlessNetClient.wxs", standalone
			if ui {
				file, code = "EndlessNet.wxs", combined
				o.ui = filepath.Join(tmp, "ui")
				if err := os.Mkdir(o.ui, 0o755); err != nil {
					t.Fatal(err)
				}
				for _, f := range uiPayload {
					putFile(t, filepath.Join(o.ui, f), []byte("UI"))
				}
				o.icon = filepath.Join(tmp, "icon.ico")
				putFile(t, o.icon, []byte("icon"))
				o.policy = map[string]any{"schema": float64(1), "profile_id": "profile", "control_origin": "https://example.org"}
			} else {
				o.cli = filepath.Join(o.bundle, "endlessnet.exe")
				putFile(t, filepath.Join(o.bundle, "private.log"), []byte("should not ship"))
			}
			if err := o.render(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(o.output, file))
			if err != nil {
				t.Fatal(err)
			}
			var tree node
			if err := xml.Unmarshal(data, &tree); err != nil {
				t.Fatal(err)
			}
			expect := func(tag, key, value, attr, want string) {
				t.Helper()
				found := tree.find(tag, key, value)
				if found == nil || found.attr(attr) != want {
					t.Fatalf("%s %s=%s: expected %s=%s", tag, key, value, attr, want)
				}
			}
			expect("Package", "", "", "UpgradeCode", code)
			expect("MajorUpgrade", "", "", "Schedule", "afterInstallInitialize")
			expect("ServiceInstall", "Id", "ClientService", "Account", "LocalSystem")
			expect("ServiceInstall", "Id", "ClientService", "Arguments", "installed-run")
			expect("Component", "Guid", uuid5(code, "directory:state"), "Permanent", "yes")
			expect("PermissionEx", "Sddl", privateACL, "Sddl", privateACL)
			expect("Custom", "Action", "PrepareMachine", "After", "CostFinalize")
			expect("CustomAction", "Id", "InitializeState", "Impersonate", "no")
			expect("Custom", "Action", "InitializeState", "Condition", "NOT Installed AND NOT ENDLESSNET_EXISTING_INSTALLATION AND NOT REMOVE")
			expect("Property", "Id", "ENSET_"+prefix+"CONNECTION_ENDPOINT", "Value", "[ENDLESSNET_LOCAL_ENDPOINT]")
			expect("Property", "Id", "ENSET_"+prefix+"LOG_LEVEL", "Value", "literal[\\[]$$X[\\]]")
			expect("Environment", "Id", "CliPath", "System", "yes")
			if ui {
				expect("ServiceInstall", "Id", "UpdateService", "Start", "demand")
				expect("PermissionEx", "Sddl", "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;LCRP;;;IU)", "Sddl", "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;LCRP;;;IU)")
				expect("Shortcut", "Id", "StartupShortcut", "Icon", "EndlessNetIcon")
				expect("Custom", "Action", "InitializeState", "After", "WriteUiPolicy")
				expect("Component", "Guid", uuid5(code, "machine-id"), "NeverOverwrite", "yes")
				putFile(t, filepath.Join(o.bundle, "INSTALL-UPDATE.PS1"), []byte("conflict"))
				if err := o.render(); err == nil {
					t.Fatal("accepted collision between bundle paths")
				}
			} else {
				expect("Launch", "", "", "Condition", "Installed OR NOT ENDLESSNET_COMBINED_PRODUCT")
				expect("Component", "Guid", uuid5(code, "installation-id"), "NeverOverwrite", "yes")
				if strings.Contains(string(data), "private.log") || tree.find("Shortcut", "", "") != nil {
					t.Fatal("standalone packaged UI or unlisted files")
				}
			}
		})
	}
}

// The build scripts use this command to prepare validation settings and WiX input.
package main

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const standalone = "{9B78E1DF-849D-588C-B5D6-DC393A1B033B}"
const combined = "{FA0F488C-9869-47EC-A2B0-F41313F40657}"
const urlNamespace = "6ba7b811-9dad-11d1-80b4-00c04fd430c8"
const publicACL = "O:SYG:SYD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FRFX;;;BU)"
const containerACL = "O:SYG:SYD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;FRFX;;;BU)"
const privateACL = "O:SYG:SYD:P(A;OICI;FA;;;SY)"
const serviceKey = "SYSTEM\\CurrentControlSet\\Services\\endlessnet-client"
const productKey = "SOFTWARE\\EndlessNet\\Client"
const prefix = "ENDLESSNET_CLIENT_"

var versionPattern = regexp.MustCompile("^v?0[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)$")
var hostPattern = regexp.MustCompile("^[a-z0-9.-]+$")
var sidPattern = regexp.MustCompile("^S-1(-[0-9]+)+$")
var settingPattern = regexp.MustCompile("^ENDLESSNET_CLIENT_[A-Z0-9_]+$")
var decimal = regexp.MustCompile("^[0-9]+$")

var payload = []string{"client.exe", "endlessnet.exe", "wintun.dll", "WINTUN-LICENSE.txt"}
var uiPayload = []string{"endlessnet_client_ui.exe", "endlessnet_updater.exe", "install-update.ps1", "release-signature.ps1", "update-worker.ps1"}

func uuidText(b []byte) string {
	value := hex.EncodeToString(b)
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
}

func uuid5(namespace, name string) string {
	bytes, err := hex.DecodeString(strings.NewReplacer("-", "", "{", "", "}", "").Replace(namespace))
	if err != nil {
		panic(err)
	}
	digest := sha1.Sum(append(bytes, name...))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	return uuidText(digest[:16])
}

func validSettings(values map[string]string) bool {
	if len(values) == 0 {
		return false
	}
	for key, value := range values {
		if !settingPattern.MatchString(key) || strings.ContainsAny(value, "\x00\r\n") {
			return false
		}
	}
	return true
}

func configuration(defaults map[string]string, server, endpoint, sid, state string) (map[string]string, error) {
	uri, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(uri.Hostname())
	if uri.Scheme != "https" || !hostPattern.MatchString(host) || uri.User != nil ||
		(uri.EscapedPath() != "" && uri.EscapedPath() != "/") || uri.ForceQuery || uri.RawQuery != "" ||
		strings.Contains(server, "#") || (uri.Port() != "" && uri.Port() != "443") {
		return nil, errors.New("server must be an HTTPS origin")
	}
	if !validSettings(defaults) {
		return nil, errors.New("invalid producer defaults")
	}
	for _, key := range []string{"INSTALLATION_ID", "ALLOWED_SIDS", "SERVICE_SID", "STATE_DIRECTORY", "CONNECTION_ENDPOINT", "CONTROL_ORIGIN"} {
		if _, found := defaults[prefix+key]; found {
			return nil, errors.New("machine and server facts must not be producer defaults")
		}
	}
	address, port, found := strings.Cut(endpoint, ":")
	ip, err := netip.ParseAddr(address)
	if !found || err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() || !validPort(port) || port != defaults[prefix+"CONNECTION_LISTEN_PORT"] {
		return nil, errors.New("expected IPv4 endpoint using the producer port")
	}
	if !sidPattern.MatchString(sid) {
		return nil, errors.New("expected Windows caller SID")
	}
	origin := "https://" + host
	if origin == "https://endlessnet.ru" {
		origin = "https://api.endlessnet.ru"
	}
	approval := origin + "/"
	if origin == "https://api.endlessnet.ru" {
		approval = "https://admin.endlessnet.ru/"
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	random[6] = (random[6] & 0x0f) | 0x40
	random[8] = (random[8] & 0x3f) | 0x80
	settings := maps.Clone(defaults)
	for key, value := range map[string]string{
		"CONTROL_ORIGIN": origin, "APPROVAL_BASE": approval, "DEFAULT_PROFILE_ID": uuid5(urlNamespace, origin),
		"INSTALLATION_ID": uuidText(random[:]), "SERVICE_SID": "S-1-5-18", "ALLOWED_SIDS": sid,
		"STATE_DIRECTORY": state, "CONNECTION_ENDPOINT": endpoint,
	} {
		settings[prefix+key] = value
	}
	return settings, nil
}

func validPort(value string) bool {
	port, err := strconv.ParseUint(value, 10, 16)
	return decimal.MatchString(value) && err == nil && port != 0
}

type element struct {
	name     xml.StartElement
	children []*element
}

func (e *element) add(tag string, args ...string) *element {
	child := &element{name: xml.StartElement{Name: xml.Name{Local: tag}}}
	for i := 0; i < len(args); i += 2 {
		child.attr(args[i], args[i+1])
	}
	e.children = append(e.children, child)
	return child
}

func (e *element) attr(key, value string) {
	e.name.Attr = append(e.name.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: value})
}

func (e *element) encode(output *xml.Encoder) error {
	if err := output.EncodeToken(e.name); err != nil {
		return err
	}
	for _, child := range e.children {
		if err := child.encode(output); err != nil {
			return err
		}
	}
	return output.EncodeToken(e.name.End())
}

func writeWiX(path string, root *element) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, headerErr := io.WriteString(file, xml.Header)
	encoder := xml.NewEncoder(file)
	encoder.Indent("", "  ")
	return errors.Join(headerErr, root.encode(encoder), encoder.Close(), file.Close())
}

func identifier(prefix, value string) string {
	hash := sha256.Sum256([]byte(value))
	return prefix + hex.EncodeToString(hash[:12])
}

func regular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("expected a regular installer input: %s", path)
	}
	return nil
}

func bundleFiles(bundle, cli, ui string) (map[string]string, error) {
	files := make(map[string]string)
	for _, name := range payload {
		source := filepath.Join(bundle, name)
		if name == "endlessnet.exe" && cli != "" {
			source = cli
		}
		if err := regular(source); err != nil {
			return nil, err
		}
		files[name] = source
	}
	if ui == "" {
		return files, nil
	}
	for _, name := range uiPayload {
		if err := regular(filepath.Join(ui, name)); err != nil {
			return nil, err
		}
	}
	clear(files)
	seen := make(map[string]bool)
	for _, root := range []string{ui, bundle} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if err := regular(path); err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			key := strings.ToLower(relative)
			if seen[key] {
				return fmt.Errorf("conflicting bundle file: %s", relative)
			}
			seen[key], files[relative] = true, path
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return files, nil
}

type renderOptions struct {
	bundle, cli, ui, icon, actions, version, output string
	service                                         map[string]string
	policy                                          map[string]any
}

func (o renderOptions) render() error {
	version := strings.TrimPrefix(o.version, "v")
	if !versionPattern.MatchString(o.version) {
		return errors.New("expected v0.MINOR.PATCH")
	}
	parts := strings.Split(version, ".")
	if _, err := strconv.ParseUint(parts[1], 10, 8); err != nil {
		return errors.New("minor version exceeds MSI limits")
	}
	if _, err := strconv.ParseUint(parts[2], 10, 16); err != nil {
		return errors.New("patch version exceeds MSI limits")
	}
	if err := regular(o.actions); err != nil {
		return err
	}
	files, err := bundleFiles(o.bundle, o.cli, o.ui)
	if err != nil {
		return err
	}
	port := o.service[prefix+"CONNECTION_LISTEN_PORT"]
	if !validSettings(o.service) || !validPort(port) {
		return errors.New("invalid service configuration")
	}
	if err := os.MkdirAll(o.output, 0o755); err != nil {
		return err
	}

	isUI := o.ui != ""
	code, product, filename := standalone, "EndlessNet Client", "EndlessNetClient.wxs"
	if isUI {
		code, product, filename = combined, "EndlessNet", "EndlessNet.wxs"
		if o.policy["schema"] != float64(1) {
			return errors.New("UI policy requires schema 1")
		}
		if _, exists := o.policy["instance_id"]; exists {
			return errors.New("UI policy cannot set instance_id")
		}
		if o.policy["profile_id"] != o.service[prefix+"DEFAULT_PROFILE_ID"] ||
			o.policy["control_origin"] != o.service[prefix+"CONTROL_ORIGIN"] {
			return errors.New("UI policy and service profile differ")
		}
		o.policy["server_sid"] = "S-1-5-18"
		if err := writeJSON(filepath.Join(o.output, "launch-policy.json"), o.policy); err != nil {
			return err
		}
		if err := regular(o.icon); err != nil {
			return err
		}
	}
	document := &element{}
	root := document.add("Wix", "xmlns", "http://wixtoolset.org/schemas/v4/wxs")
	pkg := root.add("Package", "Name", product, "Manufacturer", "EndlessNet", "Version", version, "UpgradeCode", code,
		"Scope", "perMachine", "InstallerVersion", "500", "Compressed", "yes")
	pkg.add("MajorUpgrade", "Schedule", "afterInstallInitialize", "DowngradeErrorMessage", "Установлена более новая версия "+product+".")
	pkg.add("MediaTemplate", "EmbedCab", "yes")
	if isUI {
		pkg.add("Icon", "Id", "EndlessNetIcon", "SourceFile", o.icon)
		pkg.add("Property", "Id", "ARPPRODUCTICON", "Value", "EndlessNetIcon")
		pkg.add("Upgrade", "Id", standalone).add("UpgradeVersion", "Minimum", "0.0.0", "IncludeMinimum", "yes", "Property", "ENDLESSNET_STANDALONE_PRODUCT")
	} else {
		pkg.add("Upgrade", "Id", combined).add("UpgradeVersion", "Minimum", "0.0.0", "IncludeMinimum", "yes", "OnlyDetect", "yes", "Property", "ENDLESSNET_COMBINED_PRODUCT")
		pkg.add("Launch", "Condition", "Installed OR NOT ENDLESSNET_COMBINED_PRODUCT", "Message", "Сначала удалите общую установку EndlessNet через Windows. Данные клиента сохраняются; параллельная установка одной службы запрещена.")
	}
	for _, name := range []string{"ENDLESSNET_INSTALLATION_ID", "ENDLESSNET_EXISTING_INSTALLATION", "ENDLESSNET_LOCAL_ENDPOINT"} {
		pkg.add("Property", "Id", name, "Secure", "yes")
	}
	pkg.add("Property", "Id", "EndlessNetListenPort", "Value", port)
	app := pkg.add("StandardDirectory", "Id", "ProgramFiles64Folder").add("Directory", "Id", "AppRoot", "Name", "EndlessNet")
	vendor := pkg.add("StandardDirectory", "Id", "CommonAppDataFolder").add("Directory", "Id", "DataVendor", "Name", "EndlessNet")
	data := vendor.add("Directory", "Id", "ClientData", "Name", "Client")
	state := data.add("Directory", "Id", "ClientState", "Name", "state")
	folders := []struct {
		node      *element
		name, acl string
	}{
		{app, "app", publicACL}, {vendor, "vendor", containerACL}, {data, "data", publicACL}, {state, "state", privateACL},
	}
	featureID, title := "Client", "Client service and CLI"
	var menu, startup *element
	if isUI {
		updates := data.add("Directory", "Id", "ClientUpdates", "Name", "updates")
		folders = append(folders, struct {
			node      *element
			name, acl string
		}{updates, "updates", publicACL})
		menu = pkg.add("StandardDirectory", "Id", "ProgramMenuFolder")
		startup = pkg.add("StandardDirectory", "Id", "StartupFolder")
		featureID, title = "Product", "EndlessNet"
	}
	feature := pkg.add("Feature", "Id", featureID, "Title", title, "Level", "1")
	component := func(directory *element, name string, permanent bool) *element {
		guid := uuid5(code, name)
		id := "C" + strings.ReplaceAll(guid, "-", "")
		if isUI {
			id = identifier("C", name)
		}
		node := directory.add("Component", "Id", id, "Guid", guid, "Bitness", "always64")
		if permanent {
			node.attr("Permanent", "yes")
		}
		feature.add("ComponentRef", "Id", id)
		return node
	}
	for _, folder := range folders {
		component(folder.node, "directory:"+folder.name, folder.name != "app").add("CreateFolder").add("PermissionEx", "Sddl", folder.acl)
	}
	directories := map[string]*element{"": app}
	for _, relative := range slices.Sorted(maps.Keys(files)) {
		parent := ""
		parts := strings.Split(relative, "/")
		for _, part := range parts[:len(parts)-1] {
			child := parent + "/" + part
			if directories[child] == nil {
				directories[child] = directories[parent].add("Directory", "Id", identifier("D", child), "Name", part)
			}
			parent = child
		}
		node := component(directories[parent], relative, false)
		id := identifier("F", relative)
		if !isUI {
			id = "F" + strings.ReplaceAll(uuid5(urlNamespace, relative), "-", "")
		}
		switch relative {
		case "client.exe":
			id = "ClientExe"
		case "endlessnet_client_ui.exe":
			id = "UiExe"
		case "endlessnet.exe":
			if !isUI {
				id = "CliExe"
			}
		}
		node.add("File", "Id", id, "Source", files[relative], "KeyPath", "yes").add("PermissionEx", "Sddl", publicACL)
		switch relative {
		case "client.exe":
			node.add("ServiceInstall", "Id", "ClientService", "Name", "endlessnet-client", "DisplayName", "EndlessNet Client", "Type", "ownProcess", "Start", "auto", "ErrorControl", "normal", "Account", "LocalSystem", "Arguments", "installed-run")
			node.add("ServiceControl", "Id", "ClientControl", "Name", "endlessnet-client", "Start", "install", "Stop", "both", "Remove", "uninstall", "Wait", "yes")
		case "endlessnet_updater.exe":
			node.add("ServiceInstall", "Id", "UpdateService", "Name", "endlessnet-updater", "DisplayName", "EndlessNet Updater", "Type", "ownProcess", "Start", "demand", "ErrorControl", "normal", "Account", "LocalSystem").add("PermissionEx", "Sddl", "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;LCRP;;;IU)")
			node.add("ServiceControl", "Id", "UpdateControl", "Name", "endlessnet-updater", "Stop", "both", "Remove", "uninstall", "Wait", "yes")
		case "endlessnet.exe":
			node.add("Environment", "Id", "CliPath", "Name", "PATH", "Value", "[AppRoot]", "Action", "set", "Part", "last", "System", "yes")
		}
	}
	settings := make(map[string]string)
	escape := strings.NewReplacer("$", "$$", "[", "[\\[]", "]", "[\\]]")
	for key, value := range o.service {
		settings[key] = escape.Replace(value)
	}
	for key, value := range map[string]string{
		"INSTALLATION_ID": "[ENDLESSNET_INSTALLATION_ID]", "ALLOWED_SIDS": "[UserSID]", "CONNECTION_ENDPOINT": "[ENDLESSNET_LOCAL_ENDPOINT]",
		"SERVICE_SID": "S-1-5-18", "STATE_DIRECTORY": "[CommonAppDataFolder]EndlessNet\\Client\\state",
		"RUNTIME_INSTANCE_FILE": "[CommonAppDataFolder]EndlessNet\\Client\\runtime-instance",
	} {
		settings[prefix+key] = value
	}
	registry := component(data, "service-configuration", false).add("RegistryValue", "Root", "HKLM", "Key", serviceKey, "Name", "Environment", "Type", "multiString", "KeyPath", "yes")
	for _, key := range slices.Sorted(maps.Keys(settings)) {
		prop := "ENSET_" + key
		pkg.add("Property", "Id", prop, "Value", settings[key], "Secure", "yes", "Hidden", "yes")
		registry.add("MultiStringValue", "Value", key+"=["+prop+"]")
	}
	idComponent := "installation-id"
	if isUI {
		idComponent = "machine-id"
	}
	identity := component(data, idComponent, true)
	identity.attr("NeverOverwrite", "yes")
	identity.add("RegistryValue", "Root", "HKLM", "Key", productKey, "Name", "InstallationId", "Type", "string", "Value", "[ENDLESSNET_INSTALLATION_ID]", "KeyPath", "yes")
	if isUI {
		component(data, "launch-policy", true).add("File", "Id", "LaunchPolicy", "Source", filepath.Join(o.output, "launch-policy.json"), "KeyPath", "yes").add("PermissionEx", "Sddl", publicACL)
		for _, shortcut := range []struct {
			folder           *element
			name, id, marker string
		}{
			{menu, "shortcut", "StartMenu", "Shortcut"}, {startup, "startup-shortcut", "StartupShortcut", "StartupShortcut"},
		} {
			node := component(shortcut.folder, shortcut.name, false)
			node.add("Shortcut", "Id", shortcut.id, "Name", "EndlessNet", "Target", "[#UiExe]", "WorkingDirectory", "AppRoot", "Icon", "EndlessNetIcon", "IconIndex", "0", "Arguments", "--config \"[ClientData]launch-policy.json\" --runtime-instance \"[ClientData]runtime-instance\"")
			node.add("RegistryValue", "Root", "HKLM", "Key", productKey, "Name", shortcut.marker, "Type", "integer", "Value", "1", "KeyPath", "yes")
		}
	}
	pkg.add("Binary", "Id", "InstallerActions", "SourceFile", o.actions)
	pkg.add("CustomAction", "Id", "PrepareMachine", "BinaryRef", "InstallerActions", "DllEntry", "PrepareMachine", "Return", "check")
	pkg.add("CustomAction", "Id", "InitializeState", "FileRef", "ClientExe", "ExeCommand", "installed-init", "Execute", "deferred", "Impersonate", "no", "Return", "check")
	sequence := pkg.add("InstallExecuteSequence")
	sequence.add("Custom", "Action", "PrepareMachine", "After", "CostFinalize", "Condition", "NOT REMOVE")
	sequence.add("WriteRegistryValues", "Sequence", "5810")
	after := "WriteRegistryValues"
	if isUI {
		pkg.add("CustomAction", "Id", "WriteUiPolicy", "BinaryRef", "InstallerActions", "DllEntry", "WriteUiPolicy", "Execute", "deferred", "Impersonate", "no", "Return", "check")
		sequence.add("Custom", "Action", "WriteUiPolicy", "After", after, "Condition", "NOT REMOVE")
		after = "WriteUiPolicy"
	}
	sequence.add("Custom", "Action", "InitializeState", "After", after, "Condition", "NOT Installed AND NOT ENDLESSNET_EXISTING_INSTALLATION AND NOT REMOVE")
	return writeWiX(filepath.Join(o.output, filename), root)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func readSettings(path string) (map[string]string, error) {
	var source map[string]*string
	if err := readJSON(path, &source); err != nil {
		return nil, err
	}
	settings := make(map[string]string, len(source))
	for key, value := range source {
		if value == nil {
			return nil, fmt.Errorf("expected a string setting: %s", key)
		}
		settings[key] = *value
	}
	return settings, nil
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func execute(args []string) error {
	if len(args) == 0 {
		return errors.New("expected configure or render")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	output := flags.String("output", "", "output path")
	switch args[0] {
	case "configure":
		defaults := flags.String("defaults", "", "producer defaults")
		server := flags.String("server", "", "HTTPS origin")
		endpoint := flags.String("endpoint", "", "IPv4 endpoint")
		sid := flags.String("sid", "", "caller SID")
		state := flags.String("state-directory", "", "state path")
		policy := flags.Bool("policy", false, "write service.json and policy.json to the output directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		defaultValues, err := readSettings(*defaults)
		if err != nil {
			return err
		}
		values, err := configuration(defaultValues, *server, *endpoint, *sid, *state)
		if err != nil {
			return err
		}
		if !*policy {
			return writeJSON(*output, values)
		}
		if err := os.MkdirAll(*output, 0o755); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(*output, "service.json"), values); err != nil {
			return err
		}
		return writeJSON(filepath.Join(*output, "policy.json"), map[string]any{
			"schema": 1, "profile_id": values[prefix+"DEFAULT_PROFILE_ID"], "control_origin": values[prefix+"CONTROL_ORIGIN"],
			"server_sid": "S-1-5-18", "connect_timeout_ms": 2000, "write_timeout_ms": 2000, "rpc_timeout_ms": 30000,
			"poll_interval_seconds": 2, "journal_max_bytes": 65536, "pending_write_max_bytes": 262144, "failure_max_bytes": 16384,
		})
	case "render":
		o := renderOptions{}
		flags.StringVar(&o.bundle, "bundle", "", "Client bundle")
		flags.StringVar(&o.cli, "cli", "", "standalone CLI")
		flags.StringVar(&o.ui, "ui", "", "UI bundle")
		flags.StringVar(&o.icon, "icon", "", "UI icon")
		flags.StringVar(&o.actions, "actions", "", "native actions")
		flags.StringVar(&o.version, "version", "", "MSI version")
		service := flags.String("service-profile", "", "service profile")
		policy := flags.String("ui-policy", "", "UI policy")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		o.output = *output
		if o.bundle == "" || o.output == "" || (o.ui == "" && o.cli == "") {
			return errors.New("missing installer input")
		}
		var err error
		o.service, err = readSettings(*service)
		if err != nil {
			return err
		}
		if o.ui != "" {
			if err := readJSON(*policy, &o.policy); err != nil {
				return err
			}
		}
		return o.render()
	default:
		return errors.New("expected configure or render")
	}
}

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

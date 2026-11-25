package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const clientSDK = "github.com/endless-net/client/api"
const clientSDKVersion = "v0.8.0"

var digestPattern = regexp.MustCompile("^[0-9a-f]{64}$")

type modulePin struct{ Path, Version string }
type moduleMetadata struct {
	Require []modulePin
	Replace []struct{ Old, New modulePin }
}

func (r runner) metadata(ctx context.Context, where string) (moduleMetadata, error) {
	var result moduleMetadata
	err := r.goJSON(ctx, where, &result, "mod", "edit", "-json")
	return result, err
}

func (r runner) goJSON(ctx context.Context, where string, result any, args ...string) error {
	data, err := r.run(ctx, where, goEnvironment(), append([]string{"go"}, args...)...)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}

func extractConstant(file, key string, length int) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	pattern := regexp.MustCompile(fmt.Sprintf("const %s = \"([0-9a-f]{%d})\"", key, length))
	matches := pattern.FindAllStringSubmatch(string(data), -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("ambiguous contract constant %s", key)
	}
	return matches[0][1], nil
}

func (r runner) verifyClientContract(ctx context.Context) error {
	main := filepath.Join(r.root, "cmd/client/main.go")
	revision, err := extractConstant(main, "clientAPIRevision", 40)
	if err != nil {
		return err
	}
	digest, err := extractConstant(main, "clientContractSHA256", 64)
	if err != nil {
		return err
	}
	console, err := extractConstant(filepath.Join(r.root, "cmd/endlessnet/main.go"), "contractSHA256", 64)
	if err != nil {
		return err
	}
	if console != digest {
		return errors.New("console contract differs from Client service")
	}
	var module struct {
		Version, Dir string
		Replace      *modulePin
	}
	if err := r.goJSON(ctx, r.root, &module, "list", "-m", "-json", clientSDK); err != nil {
		return err
	}
	if module.Replace != nil || module.Version != clientSDKVersion || module.Dir == "" {
		return errors.New("the Client SDK pin differs from compiled identity")
	}
	var download downloadInfo
	if err := r.goJSON(ctx, r.root, &download, "mod", "download", "-json", clientSDK+"@"+clientSDKVersion); err != nil {
		return err
	}
	if download.Path != clientSDK || download.Version != clientSDKVersion {
		return errors.New("the Client SDK download identity differs")
	}
	origin, err := download.verifiedOrigin()
	if err != nil {
		return err
	}
	if origin["Hash"] != revision {
		return errors.New("the Client SDK origin differs from compiled identity")
	}
	moduleDir, err := filepath.EvalSymlinks(module.Dir)
	if err != nil {
		return err
	}
	downloadDir, err := filepath.EvalSymlinks(download.Dir)
	if err != nil {
		return err
	}
	if moduleDir != downloadDir {
		return errors.New("the Client SDK resolved directory differs")
	}
	actual, err := fileDigest(filepath.Join(moduleDir, "client.binpb"))
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", actual) != digest {
		return errors.New("compiled Client contract digest differs from published descriptor")
	}
	return nil
}

var cryptoPaths = map[string]string{
	"github.com/amnezia-vpn/amneziawg-go/v3": "third_party/vendor/amneziawg",
	"github.com/tailscale/wireguard-go":      "third_party/vendor/wireguard",
}

type cryptoPin struct {
	Path      string            `json:"path"`
	Version   string            `json:"version"`
	Directory string            `json:"directory"`
	Sum       string            `json:"sum"`
	GoModSum  string            `json:"go_mod_sum"`
	Origin    map[string]string `json:"origin"`
	Upstream  map[string]string `json:"upstream_files"`
	Patched   map[string]string `json:"patched_files"`
}

type downloadInfo struct {
	Path, Version, Dir, Info, Zip, Sum, GoModSum string
	Origin                                       json.RawMessage
}

func (d downloadInfo) verifiedOrigin() (map[string]string, error) {
	var info struct {
		Version string
		Origin  map[string]string
	}
	data, err := os.ReadFile(d.Info)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	var origin map[string]string
	if d.Origin != nil {
		if err := json.Unmarshal(d.Origin, &origin); err != nil {
			return nil, err
		}
	}
	if info.Version != d.Version || len(info.Origin) == 0 ||
		(d.Origin != nil && !maps.Equal(origin, info.Origin)) {
		return nil, errors.New("published module origin differs")
	}
	return info.Origin, nil
}

func cryptoManifest(root string) ([]cryptoPin, error) {
	var manifest struct {
		Schema  int
		Modules []cryptoPin
	}
	data, err := os.ReadFile(filepath.Join(root, "third_party/sources.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodeJSON(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Schema != 1 || len(manifest.Modules) != len(cryptoPaths) {
		return nil, errors.New("invalid crypto source manifest")
	}
	return manifest.Modules, nil
}

func (r runner) verifyCrypto(ctx context.Context, where string, metadata moduleMetadata, fetch bool) ([]cryptoPin, error) {
	pins, err := cryptoManifest(where)
	if err != nil {
		return nil, err
	}
	requires := make(map[string]string)
	replacements := make(map[string]modulePin)
	for _, require := range metadata.Require {
		requires[require.Path] = require.Version
	}
	for _, replace := range metadata.Replace {
		if _, ok := cryptoPaths[replace.Old.Path]; ok {
			if replace.Old.Version != "" {
				return nil, errors.New("version-specific crypto replacement")
			}
			replacements[replace.Old.Path] = replace.New
		}
	}
	if len(pins) != len(replacements) {
		return nil, errors.New("crypto source and replacement inventories differ")
	}
	seen := make(map[string]bool)
	for _, pin := range pins {
		directory, known := cryptoPaths[pin.Path]
		if !known || seen[pin.Path] || directory != pin.Directory || requires[pin.Path] != pin.Version ||
			replacements[pin.Path] != (modulePin{Path: "./" + directory}) ||
			pin.Origin["VCS"] != "git" || !commitSHA.MatchString(pin.Origin["Hash"]) || pin.Upstream["LICENSE"] == "" {
			return nil, fmt.Errorf("crypto pin or provenance differs: %s", pin.Path)
		}
		seen[pin.Path] = true
		for file := range pin.Patched {
			switch file {
			case "device/device.go", "device/peer.go", "device/send.go", "device/receive.go", "device/packet_hooks.go":
			default:
				return nil, fmt.Errorf("patch outside packet metadata paths: %s", file)
			}
		}
		vendor, err := localPath(where, directory)
		if err != nil {
			return nil, err
		}
		expected := maps.Clone(pin.Upstream)
		maps.Copy(expected, pin.Patched)
		if err := verifyCryptoFiles(vendor, expected); err != nil {
			return nil, err
		}
		if fetch {
			if err := r.verifyUpstream(ctx, where, vendor, pin); err != nil {
				return nil, err
			}
		}
	}
	return pins, nil
}

func verifyCryptoFiles(root string, expected map[string]string) error {
	count := 0
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		want := expected[filepath.ToSlash(relative)]
		got, err := fileDigest(file)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", got) != want {
			return fmt.Errorf("crypto source checksum differs: %s", relative)
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if count != len(expected) {
		return errors.New("third-party source inventory differs")
	}
	return nil
}

func upstreamZIP(data []byte, pin cryptoPin) (map[string][]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := slices.Clone(archive.File)
	slices.SortFunc(files, func(a, b *zip.File) int { return strings.Compare(a.Name, b.Name) })
	hash := sha256.New()
	contents := make(map[string][]byte)
	last := ""
	prefix := pin.Path + "@" + pin.Version + "/"
	for _, file := range files {
		relative, ok := strings.CutPrefix(file.Name, prefix)
		if !ok || file.Name == last || strings.ContainsAny(relative, "\\\n") || !fs.ValidPath(strings.TrimSuffix(relative, "/")) {
			return nil, errors.New("invalid or duplicate upstream module path")
		}
		last = file.Name
		content, err := zipContents(file)
		if err != nil {
			return nil, err
		}
		_, _ = fmt.Fprintf(hash, "%x  %s\n", sha256.Sum256(content), file.Name)
		if !file.FileInfo().IsDir() && !slices.Contains(strings.Split(relative, "/"), ".github") && !strings.EqualFold(path.Ext(relative), ".md") {
			contents[relative] = content
		}
	}
	if "h1:"+base64.StdEncoding.EncodeToString(hash.Sum(nil)) != pin.Sum {
		return nil, errors.New("upstream module h1 mismatch")
	}
	return contents, nil
}

func zipContents(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(reader)
	return data, errors.Join(err, reader.Close())
}

func (r runner) verifyUpstream(ctx context.Context, where, vendor string, pin cryptoPin) error {
	var download downloadInfo
	if err := r.goJSON(ctx, where, &download, "mod", "download", "-json", pin.Path+"@"+pin.Version); err != nil {
		return err
	}
	if download.Path != pin.Path || download.Version != pin.Version || download.Sum != pin.Sum || download.GoModSum != pin.GoModSum {
		return errors.New("published crypto checksum differs")
	}
	origin, err := download.verifiedOrigin()
	if err != nil {
		return err
	}
	if !maps.Equal(origin, pin.Origin) {
		return errors.New("published crypto origin differs")
	}
	data, err := os.ReadFile(download.Zip)
	if err != nil {
		return err
	}
	upstream, err := upstreamZIP(data, pin)
	if err != nil {
		return err
	}
	inventory := make(map[string]string)
	for name, data := range upstream {
		inventory[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	if !maps.Equal(inventory, pin.Upstream) {
		return errors.New("upstream file inventory differs from pinned ZIP")
	}
	for filename, method := range map[string]string{"device/send.go": "RoutineEncryption", "device/receive.go": "RoutineDecryption"} {
		original, ok := upstream[filename]
		if !ok {
			continue
		}
		local, err := os.ReadFile(filepath.Join(vendor, filename))
		if err != nil {
			return err
		}
		before, err := worker(original, method)
		if err != nil {
			return err
		}
		after, err := worker(local, method)
		if err != nil {
			return err
		}
		if !bytes.Equal(before, after) {
			return fmt.Errorf("cryptographic worker changed: %s", method)
		}
	}
	return nil
}

func worker(source []byte, method string) ([]byte, error) {
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, "worker.go", source, 0)
	if err != nil {
		return nil, err
	}
	var found []byte
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != method {
			continue
		}
		if found != nil {
			return nil, errors.New("ambiguous crypto worker")
		}
		found = source[files.Position(function.Pos()).Offset:files.Position(function.End()).Offset]
	}
	if found == nil {
		return nil, errors.New("missing crypto worker")
	}
	return found, nil
}

func (r runner) auditCrypto(ctx context.Context, root string, pins []cryptoPin) error {
	tmp, err := os.MkdirTemp("", "endlessnet-crypto-audit-")
	if err != nil {
		return err
	}
	defer cleanup(tmp)
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
			return err
		}
	}
	edit := []string{"go", "mod", "edit", "-module=endlessnet.invalid/crypto-audit"}
	download := []string{"go", "mod", "download"}
	imports := "package audit\nimport (\n"
	for _, pin := range pins {
		edit = append(edit, "-dropreplace="+pin.Path)
		download = append(download, pin.Path)
		imports += fmt.Sprintf("_ %q\n", pin.Path+"/device")
	}
	if err := os.WriteFile(filepath.Join(tmp, "audit.go"), []byte(imports+")\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{edit, download, {"govulncheck", "-scan=package", "./..."}} {
		output, err := r.run(ctx, tmp, goEnvironment(), args...)
		if err != nil {
			return err
		}
		fmt.Print(string(output))
	}
	return nil
}

func cryptoNotices(root string) ([]archiveEntry, error) {
	pins, err := cryptoManifest(root)
	if err != nil || len(pins) == 0 {
		return nil, err
	}
	var files []archiveEntry
	var provenance []map[string]any
	for _, pin := range pins {
		file, err := localPath(root, pin.Directory+"/LICENSE")
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		files = append(files, archiveEntry{name: "licenses/" + path.Base(pin.Directory) + "-LICENSE.txt", content: content, mode: 0o644})
		provenance = append(provenance, map[string]any{"path": pin.Path, "version": pin.Version, "sum": pin.Sum, "origin": pin.Origin, "patched_files": pin.Patched})
	}
	data, err := json.Marshal(provenance)
	return append(files, archiveEntry{name: "crypto-sources.json", content: append(data, '\n'), mode: 0o644}), err
}

type wintunFile struct {
	Entry  string
	Size   int64
	SHA256 string
}
type wintunManifest struct {
	Version, URL, SHA256 string
	Size                 int64
	Files                map[string]wintunFile
}

var driverVersion = regexp.MustCompile("^[0-9]+[.][0-9]+[.][0-9]+$")

func wintunEntries(ctx context.Context, root string, t target) ([]archiveEntry, error) {
	if t != (target{OS: "windows", Arch: "amd64"}) {
		return nil, errors.New("the Wintun runtime requires Windows amd64")
	}
	var manifest wintunManifest
	data, err := os.ReadFile(filepath.Join(root, "internal/wintundriver/manifest.json"))
	if err != nil {
		return nil, err
	}
	if err := decodeJSON(data, &manifest); err != nil {
		return nil, err
	}
	prefix := "https://www.wintun.net/builds/wintun-"
	if !driverVersion.MatchString(manifest.Version) || manifest.URL != prefix+manifest.Version+".zip" ||
		manifest.Size <= 0 || !digestPattern.MatchString(manifest.SHA256) || len(manifest.Files) != 2 {
		return nil, errors.New("invalid pinned Wintun manifest")
	}
	expected := map[string]string{"wintun.dll": "wintun/bin/amd64/wintun.dll", "WINTUN-LICENSE.txt": "wintun/LICENSE.txt"}
	for name, path := range expected {
		file := manifest.Files[name]
		if file.Entry != path || file.Size <= 0 || file.Size > manifest.Size || !digestPattern.MatchString(file.SHA256) {
			return nil, errors.New("invalid pinned Wintun file")
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "EndlessNet-Client-Build")
	client := http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, manifest.Size+1))
	if err = errors.Join(err, response.Body.Close()); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK || int64(len(data)) != manifest.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != manifest.SHA256 {
		return nil, errors.New("pinned Wintun archive mismatch")
	}
	return extractWintun(data, manifest)
}

func extractWintun(data []byte, manifest wintunManifest) ([]archiveEntry, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := make(map[string]*zip.File)
	for _, file := range archive.File {
		if files[file.Name] != nil {
			return nil, errors.New("duplicate Wintun archive entry")
		}
		files[file.Name] = file
	}
	var entries []archiveEntry
	for name, expected := range manifest.Files {
		file := files[expected.Entry]
		if file == nil || file.FileInfo().IsDir() || file.UncompressedSize64 != uint64(expected.Size) {
			return nil, errors.New("pinned Wintun entry size differs")
		}
		content, err := zipContents(file)
		if err != nil {
			return nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(content)) != expected.SHA256 {
			return nil, errors.New("pinned Wintun entry checksum differs")
		}
		entries = append(entries, archiveEntry{name: name, content: content, mode: 0o644})
	}
	return entries, nil
}

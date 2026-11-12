"""Unit checks for the autonomous public CI contract; no network or real releases."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("public_ci", Path(__file__).with_name("ci.py"))
ci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ci)


class PublicCI(unittest.TestCase):
    def test_crypto_origin_cold_proxy_info_and_warm_reply_require_exact_provenance(self):
        origin = {"VCS": "git", "URL": "https://github.com/example/crypto",
                  "Hash": "a" * 40, "Ref": "refs/tags/v0.1.0"}
        entry = {"path": "example.org/crypto", "version": "v0.1.0", "origin": origin}
        for failure in (None, "cold", "reply-hash", "reply-vcs", "reply-url", "reply-ref",
                        "info-hash", "info-vcs", "info-url", "info-ref", "missing-origin",
                        "missing-info", "wrong-info-version", "wrong-reply-version", "wrong-path", "null-reply-origin", "extra-origin"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                info = {"Version": entry["version"], "Origin": dict(origin)}
                path = Path(temporary) / "module.info"
                downloaded = {"Path": entry["path"], "Version": entry["version"],
                              "Info": str(path), "Origin": dict(origin)}
                if failure == "cold":
                    del downloaded["Origin"]
                elif failure and failure.startswith("reply-") and failure != "wrong-reply-version":
                    key = {"hash": "Hash", "vcs": "VCS", "url": "URL", "ref": "Ref"}[failure[6:]]
                    downloaded["Origin"][key] = "different"
                elif failure and failure.startswith("info-"):
                    key = {"hash": "Hash", "vcs": "VCS", "url": "URL", "ref": "Ref"}[failure[5:]]
                    info["Origin"][key] = "different"
                elif failure == "missing-origin":
                    del info["Origin"]
                    del downloaded["Origin"]
                elif failure == "missing-info":
                    del downloaded["Info"]
                elif failure == "wrong-info-version":
                    info["Version"] = "v9.0.0"
                elif failure == "wrong-reply-version":
                    downloaded["Version"] = "v9.0.0"
                elif failure == "wrong-path":
                    downloaded["Path"] = "example.org/other"
                elif failure == "null-reply-origin":
                    downloaded["Origin"] = None
                elif failure == "extra-origin":
                    info["Origin"]["TagSum"] = "unreviewed"
                path.write_text(json.dumps(info), encoding="utf-8")
                if failure in (None, "cold"):
                    ci.verify_crypto_origin(downloaded, entry)
                else:
                    with self.assertRaises(ValueError):
                        ci.verify_crypto_origin(downloaded, entry)

    def crypto_fixture(self, root):
        entries = []
        originals = {}
        for module, directory in ci.CRYPTO_SOURCES.items():
            worker = b"\nfunc (d *Device) RoutineEncryption() {\n crypto()\n}\n"
            files = {"LICENSE": b"MIT fixture", "device/noise.go": b"unchanged crypto", "device/send.go": b"original queue" + worker}
            where = root / directory
            for name, data in files.items():
                (where / name).parent.mkdir(parents=True, exist_ok=True)
                (where / name).write_bytes(data)
            patched = b"annotated queue" + worker
            (where / "device/send.go").write_bytes(patched)
            entries.append({"path": module, "version": "v3.1.0" if module.endswith("/v3") else "v0.1.0",
                            "directory": directory, "sum": "h1:fixture", "go_mod_sum": "h1:modfixture",
                            "origin": {"VCS": "git", "Hash": "a" * 40},
                            "upstream_files": {n: ci.hashlib.sha256(d).hexdigest() for n, d in files.items()},
                            "patched_files": {"device/send.go": ci.hashlib.sha256(patched).hexdigest()}})
            originals[module] = files
        (root / "third_party/sources.json").write_text(json.dumps({"schema": 1, "modules": entries}))
        metadata = {"Require": [{"Path": e["path"], "Version": e["version"]} for e in entries],
                    "Replace": [{"Old": {"Path": e["path"]}, "New": {"Path": "./" + e["directory"]}} for e in entries]}
        return entries, originals, metadata


    def test_crypto_sources_reject_tampering_and_unreviewed_paths(self):
        for failure in (None, "tamper", "extra", "noise", "pin", "missing", "traversal", "symlink", "crypto-worker"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                entries, originals, metadata = self.crypto_fixture(root)
                entry = entries[0]
                where = root / entry["directory"]
                if failure == "tamper":
                    (where / "device/noise.go").write_bytes(b"changed")
                elif failure == "extra":
                    (where / "injected.go").write_bytes(b"extra")
                elif failure == "noise":
                    entry["patched_files"]["device/noise.go"] = entry["upstream_files"]["device/noise.go"]
                elif failure == "pin":
                    metadata["Require"][0]["Version"] = "v9.0.0"
                elif failure == "missing":
                    (root / "third_party/sources.json").unlink()
                elif failure == "traversal":
                    entry["upstream_files"]["../escaped.go"] = "b" * 64
                elif failure == "symlink":
                    if os.name != "posix":
                        continue
                    (where / "link.go").symlink_to(where / "device/noise.go")
                elif failure == "crypto-worker":
                    changed = (where / "device/send.go").read_bytes().replace(b"crypto()", b"changed()")
                    (where / "device/send.go").write_bytes(changed)
                    entry["patched_files"]["device/send.go"] = ci.hashlib.sha256(changed).hexdigest()
                if failure not in ("missing",):
                    (root / "third_party/sources.json").write_text(json.dumps({"schema": 1, "modules": entries}))
                if failure:
                    with self.assertRaises(ValueError):
                        if failure == "crypto-worker":
                            ci.verify_crypto_module(root, entry, originals[entry["path"]])
                        else:
                            ci.verify_crypto_sources(root, metadata, fetch=False)
                else:
                    self.assertEqual(ci.verify_crypto_sources(root, metadata, fetch=False), entries)
                    for e in entries:
                        ci.verify_crypto_module(root, e, originals[e["path"]])
                    notices = ci.crypto_notices(root)
                    self.assertEqual(set(notices), {"crypto-sources.json", "licenses/amneziawg-LICENSE.txt", "licenses/wireguard-LICENSE.txt"})
                    self.assertEqual(notices["licenses/amneziawg-LICENSE.txt"][0], b"MIT fixture")
                    self.assertEqual(json.loads(notices["crypto-sources.json"][0])[0]["patched_files"], entry["patched_files"])


    def test_upstream_module_zip_checks_go_content_hash_and_inventory(self):
        module, version = "example.org/fixture", "v0.1.0"
        files = {"LICENSE": b"MIT fixture", "README.md": b"not shipped", ".github/test.yml": b"not shipped", "device/noise.go": b"crypto"}
        digest = ci.hashlib.sha256()
        data = ci.io.BytesIO()
        with ci.zipfile.ZipFile(data, "w") as archive:
            for name, content in sorted(files.items()):
                full = module + "@" + version + "/" + name
                archive.writestr(full, content)
                digest.update((ci.hashlib.sha256(content).hexdigest() + "  " + full + "\n").encode())
        checksum = "h1:" + ci.base64.b64encode(digest.digest()).decode()
        self.assertEqual(ci.module_zip_files(data.getvalue(), module, version, checksum),
                         {k: v for k, v in files.items() if k in {"LICENSE", "device/noise.go"}})
        with self.assertRaisesRegex(ValueError, "h1 mismatch"):
            ci.module_zip_files(data.getvalue(), module, version, "h1:wrong")
        with self.assertRaisesRegex(ValueError, "invalid upstream module path"):
            ci.module_zip_files(data.getvalue(), "example.org/other", version, checksum)


    def test_upstream_crypto_audit_uses_original_versions_outside_checkout(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            entries, _, _ = self.crypto_fixture(root)
            (root / "go.mod").write_text("module fixture\n")
            calls = []
            def record(args, **kwargs):
                where = kwargs["cwd"]
                self.assertFalse(where.is_relative_to(root))
                self.assertEqual(kwargs["env"]["GOWORK"], "off")
                if args[0] == "govulncheck":
                    self.assertEqual(args, ["govulncheck", "-scan=package", "./..."])
                    source = (where / "audit.go").read_text()
                    for entry in entries:
                        self.assertIn(entry["path"] + "/device", source)
                calls.append(args)
                return ""
            with patch.object(ci, "command", side_effect=record):
                ci.audit_upstream_crypto(root, entries)
            for entry in entries:
                self.assertIn("-dropreplace=" + entry["path"], calls[0])
            self.assertEqual(calls[1], ["go", "mod", "download", *[e["path"] for e in entries]])
            self.assertEqual((root / "go.mod").read_text(), "module fixture\n")


    @unittest.skipUnless(os.name == "posix", "Bash fetch contract runs in Ubuntu CI")
    def test_private_api_fetch_denies_before_download_without_leaving_helper(self):
        script = Path(__file__).with_name("fetch-private-api.sh").resolve()
        for token, event in (("", "push"), ("fixture-token", "pull_request_target")):
            with self.subTest(event=event), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                go = root / "go"
                go.write_text("#!/bin/sh\nexit 97\n", encoding="utf-8")
                go.chmod(0o700)
                env = {"PATH": str(root) + ":/usr/bin:/bin", "TMPDIR": str(root),
                       "ENDLESSNET_API_READ_TOKEN": token, "GITHUB_EVENT_NAME": event}
                result = subprocess.run(["bash", str(script)], cwd=root, env=env,
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(list(root.iterdir()), [go])
                self.assertNotIn("fixture-token", result.stdout + result.stderr)

    @unittest.skipUnless(os.name == "posix", "Bash fetch contract runs in Ubuntu CI")
    def test_private_api_fetch_uses_ephemeral_auth_and_cleans_up_on_download_failure(self):
        script = Path(__file__).with_name("fetch-private-api.sh").resolve()
        for download_exit in (0, 42):
            with self.subTest(download_exit=download_exit), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                go = root / "go"
                go.write_text('''#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'mod download all' ]]
[[ "$GOWORK" == off && "$GIT_TERMINAL_PROMPT" == 0 ]]
[[ "$GOPRIVATE" == 'github.com/endless-net/*' ]]
[[ "$GONOPROXY" == "$GOPRIVATE" && "$GONOSUMDB" == "$GOPRIVATE" ]]
[[ "$GIT_CONFIG_KEY_0" == credential.helper && -z "$GIT_CONFIG_VALUE_0" ]]
[[ "$GIT_CONFIG_KEY_1" == core.askPass && "$GIT_CONFIG_VALUE_1" == "$GIT_ASKPASS" ]]
[[ "$GIT_CONFIG_KEY_2" == credential.useHttpPath && "$GIT_CONFIG_VALUE_2" == false ]]
[[ "$(stat -c %a "$GIT_ASKPASS")" == 700 ]]
[[ "$("$GIT_ASKPASS" "Username for 'https://github.com': ")" == x-access-token ]]
[[ "$("$GIT_ASKPASS" "Password for 'https://x-access-token@github.com': ")" == "$ENDLESSNET_API_READ_TOKEN" ]]
if "$GIT_ASKPASS" "Password for 'https://other.example': "; then exit 98; fi
exit "$FIXTURE_DOWNLOAD_EXIT"
''', encoding="utf-8")
                go.chmod(0o700)
                env = {"PATH": str(root) + ":/usr/bin:/bin", "TMPDIR": str(root),
                       "ENDLESSNET_API_READ_TOKEN": "fixture-token", "GITHUB_EVENT_NAME": "push",
                       "FIXTURE_DOWNLOAD_EXIT": str(download_exit), "GOWORK": "ambient-workspace"}
                result = subprocess.run(["bash", str(script)], cwd=root, env=env,
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, download_exit, result.stderr)
                self.assertEqual(list(root.iterdir()), [go])
                self.assertNotIn("fixture-token", result.stdout + result.stderr)

    def test_windows_packaging_binds_identity_to_clean_commit_and_tag(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "repo"
            sha = self.committed_checkout(root)
            defaults = root / "packaging/windows/install-defaults.json"
            defaults.parent.mkdir(parents=True)
            defaults.write_bytes(b"{}")
            ci.command(["git", "add", "packaging"], cwd=root)
            ci.command(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "default settings fixture"], cwd=root)
            sha = ci.command(["git", "rev-parse", "HEAD"], cwd=root, capture=True)
            date = ci.command(["git", "show", "-s", "--format=%cI", sha], cwd=root, capture=True)
            profile = {"component": "client", "artifacts": [{
                "release": "runtime", "kind": "go", "name": "client", "module": ".",
                "package": "./cmd/client", "target": {"os": "windows", "arch": "amd64"}}]}
            real_command = ci.command
            def compile_fixture(args, **kwargs):
                if args[:2] == ["go", "build"]:
                    if args[-1] == "./cmd/client":
                        flags = args[args.index("-ldflags") + 1]
                        self.assertEqual(flags, "-X main.version=v0.7.0 -X main.commit=" + sha
                                         + " -X main.buildDate=" + date)
                    else:
                        self.assertEqual(args[-1], "./cmd/endlessnet")
                        self.assertNotIn("-ldflags", args)
                    self.assertEqual(kwargs["env"]["GOOS"], "windows")
                    Path(args[args.index("-o") + 1]).write_bytes(b"compiled fixture")
                    return ""
                return real_command(args, **kwargs)
            native = {"wintun.dll": (b"pinned dll fixture", 0o644), "WINTUN-LICENSE.txt": (b"license fixture", 0o644)}
            with patch.object(ci, "command", side_effect=compile_fixture), patch.object(ci, "verify_client_contract") as verify, patch.object(ci, "wintun_files", return_value=native) as driver:
                ci.build(root, profile, root / ".artifacts", "v0.7.0")
                verify.assert_called_once_with(root)
                driver.assert_called_once_with(root, {"os": "windows", "arch": "amd64"})
            with ci.zipfile.ZipFile(root / ".artifacts/client-v0.7.0-windows-amd64.zip") as archive:
                self.assertEqual(set(archive.namelist()), {"client.exe", "endlessnet.exe", "install-defaults.json", *native})
                self.assertEqual(archive.read("endlessnet.exe"), b"compiled fixture")
                self.assertEqual(archive.read("wintun.dll"), b"pinned dll fixture")
                self.assertEqual(archive.read("install-defaults.json"), b"{}")

    def test_wintun_pins_archive_entries_and_license_without_network(self):
        for failure in (None, "archive", "size", "dll", "license", "duplicate"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                data = ci.io.BytesIO()
                files = {"wintun.dll": b"signed binary fixture", "WINTUN-LICENSE.txt": b"license fixture"}
                entries = {"wintun.dll": "wintun/bin/amd64/wintun.dll", "WINTUN-LICENSE.txt": "wintun/LICENSE.txt"}
                with ci.zipfile.ZipFile(data, "w") as archive:
                    for name, content in files.items():
                        archive.writestr(entries[name], content)
                    if failure == "duplicate":
                        import warnings
                        with warnings.catch_warnings():
                            warnings.simplefilter("ignore", UserWarning)
                            archive.writestr(entries["wintun.dll"], files["wintun.dll"])
                payload = data.getvalue()
                manifest = {"version": "0.14.1", "url": "https://www.wintun.net/builds/wintun-0.14.1.zip",
                            "size": len(payload), "sha256": ci.hashlib.sha256(payload).hexdigest(),
                            "files": {name: {"entry": entries[name], "size": len(content),
                                             "sha256": ci.hashlib.sha256(content).hexdigest()}
                                      for name, content in files.items()}}
                if failure == "archive":
                    manifest["sha256"] = "0" * 64
                elif failure == "size":
                    manifest["size"] -= 1
                elif failure in ("dll", "license"):
                    manifest["files"]["wintun.dll" if failure == "dll" else "WINTUN-LICENSE.txt"]["sha256"] = "0" * 64
                directory = root / "internal/wintundriver"
                directory.mkdir(parents=True)
                (directory / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
                with patch.object(ci.urllib.request, "urlopen", return_value=ci.io.BytesIO(payload)) as fetch:
                    if failure:
                        with self.assertRaises(ValueError):
                            ci.wintun_files(root, {"os": "windows", "arch": "amd64"})
                    else:
                        result = ci.wintun_files(root, {"os": "windows", "arch": "amd64"})
                        self.assertEqual({name: value[0] for name, value in result.items()}, files)
                    self.assertEqual(fetch.call_count, 1)
                    self.assertEqual(fetch.call_args.kwargs, {"timeout": 30})

    def test_client_contract_requires_exact_origin_resolved_module_and_descriptor(self):
        for failure in (None, "origin", "descriptor", "replace", "directory", "version", "console"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                (root / "cmd/client").mkdir(parents=True)
                sdk = root / "sdk"
                sdk.mkdir()
                (sdk / "client.binpb").write_bytes(b"immutable descriptor fixture")
                digest = ci.hashlib.sha256((sdk / "client.binpb").read_bytes()).hexdigest()
                revision = "a" * 40
                (root / "cmd/client/main_windows.go").write_text(
                    'const clientAPIRevision = "' + revision + '"\n'
                    'const clientContractSHA256 = "' + digest + '"\n', encoding="utf-8")
                (root / "cmd/endlessnet").mkdir(parents=True)
                (root / "cmd/endlessnet/main_windows.go").write_text(
                    'const contractSHA256 = "' + ("0" * 64 if failure == "console" else digest) + '"\n', encoding="utf-8")
                module = {"Version": "v0.6.0", "Dir": str(sdk)}
                download = {"Origin": {"Hash": revision}, "Dir": str(sdk)}
                if failure == "origin":
                    download["Origin"]["Hash"] = "b" * 40
                elif failure == "descriptor":
                    (sdk / "client.binpb").write_bytes(b"mutated")
                elif failure == "replace":
                    module["Replace"] = {"Dir": str(sdk)}
                elif failure == "directory":
                    download["Dir"] = str(root / "other")
                elif failure == "version":
                    module["Version"] = "v0.1.0"
                def resolve(args, **kwargs):
                    return json.dumps(module if args[:3] == ["go", "list", "-m"] else download)
                with patch.object(ci, "command", side_effect=resolve):
                    if failure:
                        with self.assertRaises(ValueError):
                            ci.verify_client_contract(root)
                    else:
                        ci.verify_client_contract(root)

    def committed_checkout(self, root):
        root.mkdir()
        (root / ".gitignore").write_text(".artifacts/\n", encoding="utf-8")
        (root / "main.go").write_text("package main\nfunc main() {}\n", encoding="utf-8")
        for args in (["init", "-q"], ["config", "core.autocrlf", "false"],
                     ["add", "."], ["-c", "user.name=Fixture", "-c",
                     "user.email=fixture@example.invalid", "commit", "-qm", "fixture"]):
            subprocess.run(["git", *args], cwd=root, check=True, capture_output=True)
        return ci.command(["git", "rev-parse", "HEAD"], cwd=root, capture=True)

    def source_profile(self):
        return {"component": "fixture", "artifacts": [
            {"release": "runtime", "kind": "source", "name": "runtime", "path": "."},
            {"release": "api", "kind": "source", "name": "api", "path": "."}]}

    def test_build_rejects_dirty_tracked_and_untracked_inputs_before_packaging(self):
        for filename in ("main.go", "untracked.go"):
            for tag in (None, "v0.1.0", "api/v0.1.0"):
                with self.subTest(filename=filename, tag=tag), tempfile.TemporaryDirectory() as temp:
                    root = Path(temp) / "repo"
                    self.committed_checkout(root)
                    (root / filename).write_text("changed input", encoding="utf-8")
                    with patch.object(ci, "archive_bytes") as archive:
                        with self.assertRaisesRegex(ValueError, "clean") as failure:
                            ci.build(root, self.source_profile(), root / ".artifacts", tag)
                        self.assertIn(filename, str(failure.exception))
                        self.assertNotIn("changed input", str(failure.exception))
                        archive.assert_not_called()

    def test_build_rejects_source_mutation_and_commit_change_during_compilation(self):
        for mutation in ("tracked", "untracked", "commit"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                root = Path(temp) / "repo"
                self.committed_checkout(root)
                profile = {"component": "fixture", "artifacts": [{
                    "release": "runtime", "kind": "go", "name": "fixture", "module": ".",
                    "package": "./cmd/fixture", "target": {"os": "linux", "arch": "amd64"}}]}
                real_command = ci.command
                def compile_and_mutate(args, cwd=None, capture=False, env=None):
                    if args[:2] == ["go", "build"]:
                        Path(args[args.index("-o") + 1]).write_bytes(b"compiled fixture")
                        (root / ("untracked.go" if mutation == "untracked" else "main.go")).write_text(
                            "changed input", encoding="utf-8")
                        if mutation == "commit":
                            real_command(["git", "add", "."], cwd=root)
                            real_command(["git", "-c", "user.name=Fixture", "-c",
                                          "user.email=fixture@example.invalid", "commit", "-qm", "changed"], cwd=root)
                        return ""
                    return real_command(args, cwd=cwd, capture=capture, env=env)
                with patch.object(ci, "command", side_effect=compile_and_mutate):
                    with self.assertRaisesRegex(ValueError, "clean|commit changed"):
                        ci.build(root, profile, root / ".artifacts", "v0.1.0")
                self.assertFalse((root / ".artifacts/source.json").exists())

    def test_build_requires_committed_checkout_and_ignored_or_external_output(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "repo"
            self.committed_checkout(root)
            with self.assertRaisesRegex(ValueError, "output.*ignored|ignored.*output"):
                ci.build(root, self.source_profile(), root / "unignored-output")
            ci.build(root, self.source_profile(), root / ".artifacts", "v0.1.0")
            ci.build(root, self.source_profile(), Path(temp) / "external", "api/v0.1.0")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            subprocess.run(["git", "init", "-q"], cwd=root, check=True, capture_output=True)
            with self.assertRaises((ValueError, subprocess.CalledProcessError)):
                ci.build(root, self.source_profile(), root / ".artifacts")

    def test_dirty_publish_is_rejected_before_any_github_operation(self):
        for filename in ("main.go", "untracked.go"):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as temp:
                root = Path(temp) / "repo"
                sha = self.committed_checkout(root)
                output = self.output(root)
                # The output is an ignored build directory, independent of source mutations.
                output.rename(root / ".artifacts")
                output = root / ".artifacts"
                identity = json.loads((output / "source.json").read_text())
                identity["commit"] = sha
                (output / "source.json").write_text(json.dumps(identity))
                (root / filename).write_text("changed input", encoding="utf-8")
                real_run = subprocess.run
                def forbid_github(args, *positional, **kwargs):
                    if args[0] == "gh":
                        self.fail("dirty source reached a GitHub operation")
                    return real_run(args, *positional, **kwargs)
                with patch.dict(os.environ, {"GITHUB_EVENT_NAME": "push",
                        "GITHUB_REF": "refs/tags/v0.1.0", "GITHUB_REPOSITORY": "endless-net/fixture"}), patch.object(
                        ci, "remote_tag", return_value=sha), patch.object(ci.subprocess, "run", side_effect=forbid_github):
                    with self.assertRaisesRegex(ValueError, "clean"):
                        ci.publish(root, {"component": "fixture", "artifacts": [{"release": "runtime",
                            "kind": "source", "name": "fixture", "path": "."}]}, output, "v0.1.0", sha)

    def test_direct_source_archive_uses_committed_bytes_in_a_dirty_tree(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "repo"
            self.committed_checkout(root)
            committed = (root / "main.go").read_bytes()
            (root / "main.go").write_bytes(b"dirty tracked input")
            (root / "untracked.go").write_bytes(b"untracked input")
            entries = ci.archive_bytes(root, {"path": "."})
            self.assertEqual(entries["main.go"][0], committed)
            self.assertNotIn("untracked.go", entries)

    def test_owned_dart_and_flutter_packages_cannot_be_omitted(self):
        for kind in ("dart", "flutter"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                (root / "api").mkdir()
                (root / "api/pubspec.yaml").write_text("name: fixture\n")
                data = {"component": "fixture", "modules": [{"path": "api", "kind": kind}],
                        "artifacts": [{"release": "runtime", "kind": "source", "name": "fixture", "path": "api"}]}
                profile = root / "profile.json"
                profile.write_text(json.dumps(data))
                ci.load_profile(root, "profile.json")
                (root / "undeclared").mkdir()
                (root / "undeclared/pubspec.yaml").write_text("name: undeclared\n")
                with self.assertRaisesRegex(ValueError, "every owned Dart/Flutter package"):
                    ci.load_profile(root, "profile.json")
                (root / "undeclared/pubspec.yaml").unlink()
                for cache in (".git", ".artifacts", ".dart_tool", "build", "vendor", ".venv"):
                    directory = root / cache / "cache-package"
                    directory.mkdir(parents=True)
                    (directory / "pubspec.yaml").write_text("name: cache\n")
                ci.load_profile(root, "profile.json")

    def test_tags_are_normal_v0_only(self):
        for value in ("v0.1.0", "api/v0.2.3"):
            self.assertIsNotNone(ci.TAG.fullmatch(value))
        for value in ("v1.0.0", "v0.01.0", "api/v0.1.0-rc.1", "v0.1", "../v0.1.0"):
            self.assertIsNone(ci.TAG.fullmatch(value))

    def test_declared_module_cannot_disappear(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "profile.json").write_text(json.dumps({
                "component": "fixture", "modules": [{"path": ".", "kind": "go"}],
                "artifacts": [{"release": "runtime", "kind": "source",
                               "name": "fixture", "path": "."}]}))
            with self.assertRaisesRegex(ValueError, "manifest is missing"):
                ci.load_profile(root, "profile.json")

    def test_paths_cannot_escape_checkout(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "repo"
            root.mkdir()
            with self.assertRaisesRegex(ValueError, "relative path|escapes"):
                ci.local(root, "..")

    def test_archive_bytes_reproducible(self):
        with tempfile.TemporaryDirectory() as temp:
            for suffix in (".tar.gz", ".zip"):
                first, second = Path(temp) / ("first" + suffix), Path(temp) / ("second" + suffix)
                entries = {"tool": (b"binary fixture", 0o755), "nested/source": (b"source", 0o644)}
                ci.write_archive(first, entries)
                ci.write_archive(second, dict(reversed(list(entries.items()))))
                self.assertEqual(first.read_bytes(), second.read_bytes())

    def output(self, root):
        output = root / "output"
        output.mkdir()
        archive = output / "fixture-v0.1.0-source-any.tar.gz"
        archive.write_bytes(b"fixture artifact")
        digest = ci.hashlib.sha256(archive.read_bytes()).hexdigest()
        (output / "checksums.txt").write_text(digest + "  fixture-v0.1.0-source-any.tar.gz\n")
        (output / "source.json").write_text(json.dumps({
            "commit": "a" * 40, "tag": "v0.1.0", "component": "fixture"}))
        return output

    def test_checksum_tamper_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            output = self.output(Path(temp))
            (output / "fixture-v0.1.0-source-any.tar.gz").write_bytes(b"tampered")
            with self.assertRaisesRegex(ValueError, "checksum differs"):
                ci.verify_output(output, "a" * 40, "v0.1.0")

    def test_api_and_runtime_are_distinct_subsets(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "repo"
            self.committed_checkout(root)
            profile = {"component": "fixture", "artifacts": [
                {"release": "runtime", "kind": "source", "name": "runtime", "path": "."},
                {"release": "api", "kind": "source", "name": "api", "path": "."}]}
            ci.build(root, profile, root / ".artifacts", "api/v0.1.0")
            archives = [p.name for p in (root / ".artifacts").glob("*.tar.gz")]
            self.assertEqual(len(archives), 1)
            self.assertTrue(archives[0].startswith("api-"))

    def test_publish_rejects_non_tag_event(self):
        with patch.dict(os.environ, {"GITHUB_EVENT_NAME": "push",
                                   "GITHUB_REF": "refs/heads/main"}, clear=True):
            with self.assertRaisesRegex(ValueError, "matching push tag"):
                ci.publish(Path("."), {}, Path("."), "v0.1.0", "a" * 40)

    def publication(self, conflict=False, draft=False, prerelease=False, corrupt_uploaded=False, missing=False):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            output = self.output(root)
            names = {"fixture-v0.1.0-source-any.tar.gz", "checksums.txt", "source.json"}
            existing = {"tagName": "v0.1.0", "targetCommitish": "a" * 40,
                        "isDraft": draft or missing, "isPrerelease": prerelease, "assets": [{"name": name} for name in sorted(
                            {"fixture-v0.1.0-source-any.tar.gz"} if draft else names)]}
            if missing:
                existing["assets"] = []
            uploaded, edits = [], []
            def fake_command(args, cwd=None, capture=False, env=None):
                if args[:3] == ["git", "rev-parse", "HEAD"]:
                    return "a" * 40
                if args[:3] == ["gh", "release", "view"]:
                    return json.dumps(existing)
                if args[:3] == ["gh", "release", "create"]:
                    self.assertIn("--draft", args)
                    self.assertIn("--verify-tag", args)
                    return ""
                if args[:3] == ["gh", "release", "download"]:
                    name = args[args.index("--pattern") + 1]
                    directory = Path(args[args.index("--dir") + 1])
                    (directory / name).write_bytes(b"conflict" if conflict or (corrupt_uploaded and name in uploaded) else (output / name).read_bytes())
                elif args[:3] == ["gh", "release", "upload"]:
                    name = Path(args[4]).name
                    uploaded.append(name)
                    existing["assets"].append({"name": name})
                elif args[:3] == ["gh", "release", "edit"]:
                    edits.append(args)
                    existing["isDraft"] = False
                else:
                    self.fail("unexpected command " + repr(args))
                return ""
            with patch.dict(os.environ, {
                "GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v0.1.0",
                "GITHUB_REPOSITORY": "endless-net/fixture"}, clear=True), patch.object(
                    ci, "remote_tag", return_value="a" * 40), patch.object(
                    ci, "clean_commit", return_value="a" * 40), patch.object(
                    ci, "validate_output_location", return_value=output), patch.object(
                    ci, "command", side_effect=fake_command), patch.object(
                    ci.subprocess, "run", return_value=SimpleNamespace(
                        returncode=1 if missing else 0, stdout=json.dumps(existing),
                        stderr="release not found" if missing else "")):
                profile = {"component": "fixture", "artifacts": [
                    {"release": "runtime", "kind": "source", "name": "fixture", "path": "."}]}
                ci.publish(root, profile, output, "v0.1.0", "a" * 40)
            return uploaded, edits

    def test_profile_rejects_incomplete_namespace(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = self.output(Path(temporary))
            profile = {"component": "fixture", "artifacts": [
                {"release": "api", "kind": "source", "name": "api", "path": "."}]}
            with self.assertRaisesRegex(ValueError, "declared namespace"):
                ci.verify_output(output, "a" * 40, "v0.1.0", profile)

    def test_artifact_component_must_match_profile(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = self.output(Path(temporary))
            with self.assertRaisesRegex(ValueError, "component differs"):
                ci.verify_output(output, "a" * 40, "v0.1.0", {"component": "other"})

    def test_prerelease_cannot_be_an_identical_release(self):
        with self.assertRaisesRegex(ValueError, "identity differs"):
            self.publication(prerelease=True)

    def test_new_upload_bytes_are_verified(self):
        with self.assertRaisesRegex(ValueError, "uploaded asset bytes differ"):
            self.publication(draft=True, corrupt_uploaded=True)

    def test_block_style_first_party_replace_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "go.mod").write_text("module fixture\nreplace (\n github.com/endless-net/identity/api => ../other\n)\n")
            parsed = {"Replace": [{"Old": {"Path": "github.com/endless-net/identity/api"},
                                    "New": {"Path": "../other"}}]}
            with patch.object(ci, "command", return_value=json.dumps(parsed)):
                with self.assertRaisesRegex(ValueError, "replace is forbidden"):
                    ci.check(root, {"modules": [{"path": ".", "kind": "go"}]})

    def test_identical_published_retry_does_not_mutate(self):
        uploaded, edits = self.publication()
        self.assertEqual(uploaded, [])
        self.assertEqual(edits, [])

    def test_existing_asset_conflict_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "asset bytes differ"):
            self.publication(conflict=True)

    def test_interrupted_draft_only_uploads_missing(self):
        uploaded, edits = self.publication(draft=True)
        self.assertEqual(set(uploaded), {"checksums.txt", "source.json"})
        self.assertEqual(len(edits), 1)

    def test_absent_release_is_created_and_reloaded_through_draft_aware_cli(self):
        uploaded, edits = self.publication(missing=True)
        self.assertEqual(len(uploaded), 3)
        self.assertEqual(len(edits), 1)

    def test_duplicate_json_keys_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "duplicate.json"
            path.write_text('{"valid": true, "valid": false}')
            with self.assertRaisesRegex(ValueError, "duplicate JSON key"):
                ci.load_json(path)


if __name__ == "__main__":
    unittest.main()

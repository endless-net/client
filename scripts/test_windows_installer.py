import json
from pathlib import Path
import tempfile
import unittest
import xml.etree.ElementTree as ET

from windows_installer import (COMBINED_UPGRADE_CODE, NS, PRIVATE, UPGRADE_CODE,
                               configuration, render)


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        self.root = Path(self.work.name)
        self.bundle = self.root / "bundle"
        self.bundle.mkdir()
        for name in ("client.exe", "wintun.dll", "WINTUN-LICENSE.txt"):
            (self.bundle / name).write_bytes(b"unit packaging fixture")
        self.cli = self.root / "endlessnet.exe"
        self.cli.write_bytes(b"unit CLI fixture")
        self.actions = self.root / "actions.dll"
        self.actions.write_bytes(b"unit native action fixture")
        self.defaults = json.loads((Path(__file__).resolve().parents[1] / "packaging/windows/install-defaults.json").read_text(encoding="utf-8"))
        self.settings = configuration(self.defaults, "https://endlessnet.ru", "192.168.10.20:51820",
                                      "S-1-5-21-100-200-300-1001", r"C:\ProgramData\EndlessNet\Client\state")
        self.ns = {"w": NS}

    def source(self):
        return ET.parse(render(self.bundle, self.cli, self.settings, self.actions, "v0.9.0", self.root / "output"))

    def test_only_client_payload_and_machine_path(self):
        (self.bundle / "endlessnet_client_ui.exe").write_bytes(b"must not ship")
        (self.bundle / "private.log").write_text("must not ship")
        source = self.source()
        files = source.findall(".//w:File", self.ns)
        self.assertEqual({Path(f.get("Source")).name for f in files},
                         {"client.exe", "endlessnet.exe", "wintun.dll", "WINTUN-LICENSE.txt"})
        self.assertIsNone(source.find(".//w:Shortcut", self.ns))
        path = source.find(".//w:Environment", self.ns)
        self.assertEqual((path.get("Name"), path.get("Part"), path.get("System")), ("PATH", "last", "yes"))

    def test_system_service_private_state_and_initialization_fence(self):
        source = self.source()
        service = source.find(".//w:ServiceInstall", self.ns)
        self.assertEqual((service.get("Name"), service.get("Account"), service.get("Arguments")),
                         ("endlessnet-client", "LocalSystem", "installed-run"))
        acl = source.find(".//w:Directory[@Id='ClientState']/w:Component/w:CreateFolder/w:PermissionEx", self.ns)
        self.assertEqual(acl.get("Sddl"), PRIVATE)
        initializer = source.find(".//w:CustomAction[@Id='InitializeState']", self.ns)
        self.assertEqual((initializer.get("Execute"), initializer.get("Impersonate"), initializer.get("ExeCommand")),
                         ("deferred", "no", "installed-init"))
        condition = source.find(".//w:Custom[@Action='InitializeState']", self.ns).get("Condition")
        self.assertIn("NOT ENDLESSNET_EXISTING_INSTALLATION", condition)
        self.assertIn("NOT Installed", condition)
        self.assertIsNone(source.find(".//w:RemoveFolder", self.ns))

    def test_separate_product_blocks_combined_install(self):
        source = self.source()
        self.assertNotEqual(UPGRADE_CODE, COMBINED_UPGRADE_CODE)
        self.assertEqual(source.find(".//w:Package", self.ns).get("UpgradeCode"), UPGRADE_CODE)
        self.assertEqual(source.find(".//w:Upgrade", self.ns).get("Id"), COMBINED_UPGRADE_CODE)
        self.assertIn("NOT ENDLESSNET_COMBINED_PRODUCT", source.find(".//w:Launch", self.ns).get("Condition"))

    def test_identity_permanent_settings_resolved_before_removal(self):
        source = self.source()
        key = source.find(".//w:RegistryValue[@Name='InstallationId']/..", self.ns)
        self.assertEqual((key.get("Permanent"), key.get("NeverOverwrite")), ("yes", "yes"))
        self.assertEqual(source.find(".//w:MajorUpgrade", self.ns).get("Schedule"), "afterInstallInitialize")
        self.assertEqual(source.find(".//w:Custom[@Action='PrepareMachine']", self.ns).get("After"), "CostFinalize")
        settings = source.find(".//w:RegistryValue[@Name='Environment']", self.ns)
        self.assertIsNone(source.find(".//w:RegistryValue[@Name='Environment']/..", self.ns).get("NeverOverwrite"))
        entries = {v.get("Value").split("=", 1)[0]: v.get("Value").split("=", 1)[1]
                   for v in settings.findall("w:MultiStringValue", self.ns)}
        self.assertEqual(entries["ENDLESSNET_CLIENT_CONNECTION_ENDPOINT"], "[ENSET_ENDLESSNET_CLIENT_CONNECTION_ENDPOINT]")
        for name, expected in (("ALLOWED_SIDS", "[UserSID]"), ("CONNECTION_ENDPOINT", "[ENDLESSNET_LOCAL_ENDPOINT]")):
            prop = source.find(".//w:Property[@Id='ENSET_ENDLESSNET_CLIENT_" + name + "']", self.ns)
            self.assertEqual(prop.get("Value"), expected)
            self.assertEqual(prop.get("Hidden"), "yes")

    def test_configuration_fills_facts_without_test_root(self):
        self.assertEqual(self.settings["ENDLESSNET_CLIENT_CONTROL_ORIGIN"], "https://api.endlessnet.ru")
        self.assertEqual(self.settings["ENDLESSNET_CLIENT_APPROVAL_BASE"], "https://admin.endlessnet.ru/")
        self.assertEqual(self.settings["ENDLESSNET_CLIENT_ALLOWED_SIDS"], "S-1-5-21-100-200-300-1001")
        self.assertEqual(self.settings["ENDLESSNET_CLIENT_CONNECTION_ENDPOINT"], "192.168.10.20:51820")
        self.assertFalse(any("ROOT" in name for name in self.settings))
        other = configuration(self.defaults, "https://endlessnet.ru/", "192.168.10.20:51820",
                              "S-1-5-21-100-200-300-1001", r"C:\ProgramData\EndlessNet\Client\state")
        self.assertEqual(self.settings["ENDLESSNET_CLIENT_DEFAULT_PROFILE_ID"], other["ENDLESSNET_CLIENT_DEFAULT_PROFILE_ID"])
        self.assertNotEqual(self.settings["ENDLESSNET_CLIENT_INSTALLATION_ID"], other["ENDLESSNET_CLIENT_INSTALLATION_ID"])

    def test_reject_unsafe_origin_endpoint_and_machine_defaults(self):
        for server in ("http://endlessnet.ru", "https://user@endlessnet.ru", "https://endlessnet.ru/path", "https://endlessnet.ru?key=x"):
            with self.subTest(server=server), self.assertRaises(ValueError):
                configuration(self.defaults, server, "192.168.10.20:51820", "S-1-5-21-1001", "state")
        for endpoint in ("127.0.0.1:51820", "0.0.0.0:51820", "169.254.1.1:51820", "192.168.10.20:0", "192.168.10.20:51821"):
            with self.subTest(endpoint=endpoint), self.assertRaises(ValueError):
                configuration(self.defaults, "https://endlessnet.ru", endpoint, "S-1-5-21-1001", "state")
        with self.assertRaises(ValueError):
            configuration(dict(self.defaults, ENDLESSNET_CLIENT_INSTALLATION_ID="fixed"), "https://endlessnet.ru",
                          "192.168.10.20:51820", "S-1-5-21-1001", "state")

    def test_missing_cli_invalid_version_and_environment_rejected(self):
        for version in ("v1.0.0", "v0.256.0", "v0.1.65536", "0.01.2"):
            with self.subTest(version=version), self.assertRaises(ValueError):
                render(self.bundle, self.cli, self.settings, self.actions, version, self.root / "output")
        with self.assertRaises(ValueError):
            render(self.bundle, self.cli, dict(self.settings, ENDLESSNET_CLIENT_LOG_LEVEL="info\nOTHER=1"),
                   self.actions, "v0.9.0", self.root / "output")
        self.cli.unlink()
        with self.assertRaises(ValueError):
            self.source()


if __name__ == "__main__":
    unittest.main()

"""APT bootstrap commands only; fixtures never write /etc or install packages."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


@unittest.skipUnless(os.name == "posix", "Ubuntu shell fixture")
class InstallTests(unittest.TestCase):
    def run_script(self, failure="", standalone=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary = root / "bin"
            binary.mkdir()
            commands = {
                "id": "echo 0",
                "dpkg": "echo amd64",
                "install": "printf 'install %s\\n' \"$*\" >> \"$COMMAND_LOG\"",
                "apt-get": "printf 'apt %s\\n' \"$*\" >> \"$COMMAND_LOG\"; [ \"$FAILURE\" != apt ]",
                "curl": "[ \"$FAILURE\" != curl ] || exit 1; case \"$*:$FAILURE\" in *raw.githubusercontent.com*:command) exit 1;; esac; for arg do output=\"$arg\"; done; [ \"$FAILURE\" = empty ] || printf fixture-key > \"$output\"",
            }
            for name, body in commands.items():
                path = binary / name
                path.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
                path.chmod(0o755)
            log = root / "commands"
            script = Path(__file__).with_name("install.sh")
            if standalone:
                copied = root / "install.sh"
                copied.write_bytes(script.read_bytes())
                script = copied
            result = subprocess.run(
                ["sh", str(script)],
                env=dict(os.environ, PATH=str(binary) + ":" + os.environ["PATH"],
                         TMPDIR=str(root), COMMAND_LOG=str(log), FAILURE=failure),
                capture_output=True, text=True,
            )
            return result, log.read_text() if log.exists() else ""

    def test_uses_signed_by_repository_and_only_apt_package_install(self):
        result, commands = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("/etc/apt/keyrings/endlessnet.gpg", commands)
        self.assertIn("apt -o APT::Update::Error-Mode=any update", commands)
        self.assertIn("apt install -y endlessnet-client", commands)
        self.assertIn("/usr/local/bin/endlessnet", commands)

    def test_failed_or_empty_key_never_configures_or_installs(self):
        for failure in ["curl", "empty"]:
            result, commands = self.run_script(failure)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(commands, "")

    def test_apt_failure_never_reports_installation_success(self):
        result, commands = self.run_script("apt")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("apt install", commands)
        self.assertNotIn("Client установлен", result.stdout)

    def test_standalone_cli_download_failure_does_not_claim_complete_install(self):
        result, commands = self.run_script("command", standalone=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("apt install -y endlessnet-client", commands)
        self.assertNotIn("/usr/local/bin/endlessnet", commands)
        self.assertNotIn("Client установлен", result.stdout)


if __name__ == "__main__":
    unittest.main()

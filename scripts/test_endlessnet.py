"""CLI tests with isolated command fixtures, without package installation."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


@unittest.skipUnless(os.name == "posix", "Ubuntu command fixture")
class UpdateTests(unittest.TestCase):
    def run_command(self, candidate="0.4.0", answer="n\n", failure="", installed="installed 0.3.0"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "state").write_text(installed)
            commands = {
                "id": "echo 0",
                "dpkg-query": 'cat "$FIXTURE/state"',
                "dpkg": 'exec /usr/bin/dpkg "$@"',
                "apt-cache": 'printf "  Candidate: %s\\n" "$CANDIDATE"; [ "$FAILURE" != cache ]',
                "apt-get": '''printf '%s\\n' "$*" >> "$FIXTURE/commands"
if [ "$1" = install ]; then
  [ "$FAILURE" != install ] || exit 1
  [ "$FAILURE" = nochange ] || printf 'installed %s' "$CANDIDATE" > "$FIXTURE/state"
else
  [ "$FAILURE" != index ] || exit 1
fi''',
            }
            binary = root / "bin"
            binary.mkdir()
            for name, body in commands.items():
                path = binary / name
                path.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
                path.chmod(0o755)
            result = subprocess.run(
                ["sh", str(Path(__file__).with_name("endlessnet")), "check-updates"],
                env=dict(os.environ, PATH=str(binary) + ":" + os.environ["PATH"],
                         FIXTURE=str(root), CANDIDATE=candidate, FAILURE=failure),
                input=answer, capture_output=True, text=True,
            )
            log = root / "commands"
            return result, log.read_text() if log.exists() else ""

    def test_equal_or_older_candidate_never_upgrades(self):
        for candidate in ["0.3.0", "0.2.0"]:
            result, commands = self.run_command(candidate=candidate)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("Обновлений нет", result.stdout)
            self.assertNotIn("install", commands)

    def test_no_or_eof_never_installs(self):
        for answer in ["n\n", "", "\n"]:
            result, commands = self.run_command(answer=answer)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("Обновление отменено", result.stdout)
            self.assertNotIn("install", commands)

    def test_confirmation_installs_selected_version_and_checks_result(self):
        result, commands = self.run_command(answer="y\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("install --only-upgrade -y endlessnet-client=0.4.0", commands)
        self.assertIn("Client обновлён до 0.4.0", result.stdout)

    def test_source_install_and_missing_candidate_fail_without_false_success(self):
        for failure, candidate in [("index", "0.4.0"), ("install", "0.4.0"), ("nochange", "0.4.0"), ("", "(none)")]:
            result, commands = self.run_command(answer="y\n", failure=failure, candidate=candidate)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("Client обновлён", result.stdout)
            self.assertNotIn("Обновлений нет", result.stdout)
            if failure == "index" or candidate == "(none)":
                self.assertNotIn("install", commands)

    def test_unconfigured_package_is_not_repaired_or_replaced(self):
        result, commands = self.run_command(installed="half-configured 0.3.0", answer="y\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(commands, "")

    def test_partial_cache_output_with_failure_never_prompts_or_installs(self):
        result, commands = self.run_command(failure="cache", answer="y\n")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Не удалось прочитать", result.stderr)
        self.assertNotIn("Установить обновление", result.stdout)
        self.assertNotIn("install", commands)


if __name__ == "__main__":
    unittest.main()

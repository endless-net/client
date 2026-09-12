# Client installation and basic service acceptance

The `Test` workflow builds the current source and runs `tests/installation_test.go`
on fresh GitHub-hosted runners. The installation matrix is required by the
workflow's aggregate `verify` job and runs on main pushes, pull requests and
manual workflow dispatches.

| Platform | Installation under test |
| --- | --- |
| Ubuntu 22.04 and 24.04, amd64 and arm64 | Actual `.deb` from `scripts/build-deb.sh`, installed with `dpkg`, started by systemd |
| Windows Server 2022 and 2025, amd64 | Core binary with checksum-pinned, signed Wintun; generated Windows service installer and SCM |
| macOS 15, Apple Silicon and Intel | Core binary with generated launchd installer and LaunchDaemon |

The Debian test package reuses the nearest existing release tag's version; it is
built from the tested commit and never published. Windows and macOS use binaries
built from that same commit. This is source installation acceptance, not proof
that a published release manifest or signed consumer installer passed.

The suite checks:

1. Installation on an empty machine and startup of the real OS service.
2. CLI version and local IPC readiness with a bounded startup deadline.
3. `NeedsEnrollment`, no node credential, no node or peers, and an empty network list.
4. Diagnostics reporting the actual runtime OS and CPU architecture.
5. Disconnect acknowledgement and persistence of disconnected intent across a
   service restart.
6. Public CLI enrollment against the contract testserver, followed by actual
   IPv4 TCP traffic through the installed agent and a WireGuard reference peer.
7. Reinstallation of the same artifact while connected and while explicitly
   disconnected, preserving identity, trust and connection intent; explicit
   reconnect must restore real TCP traffic.
8. Removal of the service; Debian package files must also disappear. Windows
   and macOS service uninstallers deliberately retain the separately copied
   binary and state, and IPC must no longer respond.

While enrolled, the suite also stops the service in both connected and
disconnected states. Real CLI `status`, `networks`, `diagnostics`, `connect` and
`disconnect` and `events` requests use a two-second IPC timeout and must exit with code 1,
empty stdout and nonempty stderr before a separate ten-second harness deadline.
After service startup, the original identity and intent must remain, with real
TCP access restored only for connected intent. This checks service absence; it
does not establish local-user authorization or a strict process-startup latency.

Cleanup runs after failures as well. Only the test report is uploaded; private
configuration, generated identity material and raw IPC responses are not logged.
Ordinary `go test -short ./...` compiles the suite but skips machine installation.
Execution requires explicit opt-in and a GitHub-hosted disposable runner; do not
run privileged installation tests on a development workstation.

## Remaining acceptance work

Windows installation acceptance additionally executes the CLI with a restricted
version of the runner process token: Administrators is deny-only and privileges
are removed using [CreateRestrictedToken](https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-createrestrictedtoken).
The test verifies group attributes and successful CLI execution before requiring
confirmed local-forget to fail. It classifies named-pipe access denial separately
from IPC administrator authorization and rechecks identity, intent and traffic.
Only child CLI processes receive the restricted token; no account/password is
created. The account SID remains unchanged, so another owner's rights are not
tested. Windows 2022/2025 execution is pending.

The Linux/macOS suite additionally runs the installed CLI as the existing
`nobody` account. A non-root UID and successful `version` invocation exclude a
failed privilege switch or unexecutable binary as false positives. Status,
connect, disconnect and confirmed local-forget must fail at the protected Unix
IPC transport with permission denied. The privileged client must retain identity,
intent and the corresponding TCP access/denial. Hosted evidence is pending.
This is transport access denial; observer/owner/admin application-role tests
and Windows token/pipe authorization remain separate work.

- [client-ui, main](https://github.com/endless-net/client-ui/tree/main) owns the
  signed Windows MSI, UI launch and consumer installation on Windows 10/11.
- This repository owns future signed/notarized macOS packaging and additional
  Linux distributions beyond the Ubuntu amd64/arm64 matrix above.
- Client-owned gaps include installation under an unauthorized local user,
  interrupted installation, replacement with a different artifact version,
  full state removal and an actual machine reboot with late network availability.
  Service restart and same-artifact reinstall do not establish those outcomes.
- [system-tests, main](https://github.com/endless-net/system-tests/tree/main) owns
  backend enrollment, two-client tunnel traffic, DNS and route acceptance
  against immutable artifact manifests. The Client-owned installed-service TCP
  test above uses a contract testserver and a reference peer; it does not prove
  compatibility with a released backend manifest or two production participants.
- Android and iOS are not installation targets for this desktop core suite.
  Mobile acceptance needs platform applications and their installation artifacts.

These tests do not deploy production services or change runner infrastructure.
Execution evidence and its exact source/runner limits are recorded in
[the headless coverage ledger](headless-test-coverage.md). This description of
the current suite does not qualify an unexecuted source or close all HC-001–HC-065
requirements.

# Client ownership and signing-identity recovery

This repository implements the client side of the recovery protocol defined by
architecture decision `6cf37091846920e238bef631ef8951d395c084a1`. The public
control-plane contract is pinned as
`github.com/endless-net/client-api/clientapi/v2 v2.0.0-rc.4`; the local service
contract is [client IPC v2](client-ipc-v2.openapi.yaml). Neither boundary has a
legacy text or status-code compatibility path.

## Recovery state machine

The service exposes these stable state pairs:

| Service state | Control state | Meaning |
| --- | --- | --- |
| `ServerIdentityChanged` | `server_identity_changed` | A newly announced signing identity needs explicit administrator confirmation. |
| `Recovering` | `recovering` | Trust is durable and credential renewal is pending or retryable. |
| `RecoveryBlocked` | `recovery_blocked` | Protocol, binding, invalid-credential, or local validation failed without deleting state. |
| `PolicyBlocked` | `policy_blocked` | The control plane explicitly denied policy authorization. |
| `NeedsLogin` | `needs_login` | The control plane explicitly requires user authentication. |
| `NeedsEnrollment` | `not_registered` | An explicit terminal public code authorized node-bound cleanup. |

`POST /server-identity/trust` first compares both the confirmed control origin
and key ID with the current announcement. One atomic config transaction then
saves the new trust bundle and a recovery operation containing a durable
`idempotency_id`. No network synchronization can start before that transaction
commits. Retries use the same old node credential, registration binding, device
proof, and idempotency ID. A validated success commits the new credential and
signed map together.

Only `node_credential_unknown`, `node_credential_revoked`, and
`node_credential_expired` authorize deletion of node-bound state. The decision
is made exclusively with `ErrorCode.RequiresReEnrollment()` after strict v2
JSON, HTTP-status, and `X-Request-ID` validation. Unknown or malformed DTOs,
generic HTTP errors, `node_credential_invalid`, binding mismatch,
authentication, authorization, and availability failures preserve enrollment.
`diagnostic_message` is never logged as a decision, localized, or shown to a
user.

## Logout and local forget

Normal `POST /logout` first attempts remote node revocation and session logout.
If remote cleanup is not confirmed, no local credential or session is removed;
IPC returns `remote_cleanup_required`. Its `ErrorResponse.request_id` is the
authoritative correlation ID for the warning that remains after local state is
forgotten.

An administrator/root may then invoke the closed `POST /logout/local`
operation with `confirmed: true`. It does not retry remote cleanup and returns
`remote_cleanup_unconfirmed` after applying this matrix atomically:

| State | Terminal credential recovery | Explicit local forget/logout |
| --- | ---: | ---: |
| Node ID, network ID, node credential, approval/enrollment state | clear | clear |
| Cached map and map cursor/revisions | clear | clear |
| User session token and active account | retain | clear |
| Device identity and WireGuard private keys | retain | retain |
| Installation/device fingerprint | retain | retain |
| Local owner | retain | retain |
| Control/management origins and signing trust | retain | retain |
| Connection intent | retain | set `disconnected` with reason `local_logout` |

## Privileged helper contract

The helper accepts only two operations. It accepts no state path, IPC endpoint,
secret, script, shell, or arbitrary command argument.

| Platform | Installed executable | Elevation mechanism |
| --- | --- | --- |
| Windows | `C:\Program Files\EndlessNet\endlessnet-client-recovery-helper.exe` | `ShellExecuteW` with verb `runas` |
| Linux | `/usr/libexec/endlessnet/endlessnet-client-recovery-helper` | polkit action `ru.endlessnet.client.recovery`, or a direct `sudo`/root invocation |
| macOS | `/Library/PrivilegedHelperTools/ru.endlessnet.client.recovery-helper` | Authorization Services/XPC service `ru.endlessnet.client.recovery`, running as root |

Fixed arguments:

- Trust: `--operation trust-server-identity --confirmed-control-origin <origin> --confirmed-key-id <key-id>`
- Local forget: `--operation forget-local-enrollment --confirmed-local-forget`

The Windows release manifest is
`endlessnet-client_windows_amd64.manifest.json` with `schema_version: 2`.
`artifacts.recovery_helper` names the immutable release asset
`endlessnet-client-recovery-helper_windows_amd64.exe`, includes its SHA-256, and
sets `installed_name` to `endlessnet-client-recovery-helper.exe`. Installers
must not probe fallback names.

Linux packages install the helper with mode `0755` and the repository-owned
polkit policy. A macOS installer must install the signed helper at the fixed
path and register the fixed XPC identity; it must not forward arbitrary
commands or environment-provided paths.

## Events and observability

`GET /events` starts with `hello`, immediately emits a full `status_changed`,
then emits another full status only when it changes. `sequence` is local to one
stream and is not resumable. Recovery status contains only the stable error
code, correlation request ID, and retryability; secrets and public diagnostic
messages are excluded.

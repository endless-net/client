# Agents

## Git workflow

- Work directly on `main`. Do not create feature branches or pull requests.
- After completing and validating a change, commit only its intended files and
  push the commit directly to `main` immediately.
- Format every commit message according to Conventional Commits, for example
  `feat: ...`, `fix: ...`, `docs: ...`, or `chore: ...`.

## Repository boundary

- Work only within this repository.
- Before reading from or writing to any path outside this repository, request
  and receive the user's explicit permission.

- Run `git status --short` before reading or changing files and preserve existing user changes.
- Never read or print environment files, credentials, private keys, client identity keys or node credentials.
- Keep control-plane DTOs and cryptographic wire verification in the pinned `github.com/endless-net/client-api/clientapi` module; do not copy backend-internal packages.
- Do not preserve legacy behavior, deprecated interfaces, or backward compatibility.
- Do not add compatibility fallbacks for superseded CLI, IPC, state or release behavior.
- Client releases, APT packaging, Windows core artifacts and cross-platform client CI belong to this repository.
- GitHub Actions runner-unit installation, registration, systemd policy, host inventory, recovery/rollback and guarded rollout belong to `endless-net/observability`; keep only job logic and `runs-on` selectors here.
- Local verification is limited to `goimports -w .`, `go vet ./...`, `golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m`, and `go test -short ./...`.
- E2E, installer, privileged networking, release and system validation runs in GitHub pull-request or release CI.

## Version increases

- Never increase any version or generation number, including schema, configuration,
  API, protocol, contract, manifest, migration, artifact, or rollout versions,
  without the user's direct explicit permission for that exact increase.
- A request to implement, refactor, fix, remove compatibility, or make a breaking
  change does not authorize a version increase. Without explicit permission, keep
  the current version number.

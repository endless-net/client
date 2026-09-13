# Native core release pairing

The Windows core publisher now emits `client-v0.binpb`, copied byte-for-byte
from `clientipc/rpc/client.binpb` (the descriptor embedded by the native RPC
guard), and declares `ipc_version: v0`. Manifest schema 2, package versions,
release tags, executable names and attestation requirements are unchanged.
This replaces the published OpenAPI artifact; no legacy contract fallback.

The client-ui resolver must verify the immutable manifest and descriptor digest
against its generated SDK identity. Existing v2 core releases are incompatible
with that native UI. This source change does not publish or replace any release.
An independently reviewed native core artifact and platform/release acceptance
are still required. Linux manifest evolution and other release consumers remain
separate follow-up work; no cross-platform distribution acceptance is claimed.

# Native update information query

`endlessnet-client service update-info` calls `GetUpdateInfo` over the native v0
local transport and prints its protobuf JSON response unchanged. It does not
install updates, select release artifacts or infer a verified update source.

Optional `--reported-ui` accepts up to 4096 bytes of `BuildIdentity` protobuf
JSON. This is a caller claim for pairing checks, never runtime attestation.
Unknown fields, duplicate fields and malformed JSON fail before transport.
The query is not profile-scoped and does not accept mutation preconditions.

Producer testserver tests cover exact requests with absent/present UI identity,
source-unavailable and verification-failed responses, and explicit unsupported
failure without success output or retry. These are consumer/transport tests,
not evidence of a runtime update provider. `GetUpdateInfo` remains a runtime
implementation gap until authoritative signed source and pairing validation
are wired and tested; no version or contract change is implied by this command.

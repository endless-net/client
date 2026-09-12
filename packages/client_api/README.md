# endlessnet_client_api

Generated Dart/Flutter messages and gRPC client/server bindings for
`client.v0.ClientService`. Import `package:endlessnet_client_api/client_api.dart`.
The initial private package version is 0.0.0; the protocol remains v0.

The canonical source is `proto/client/v0` in this repository. Run `buf generate`
from the repository root with the generators pinned in the Protobuf CI workflow.
Do not edit `lib/src/gen` manually. CI regenerates it and runs Dart analysis.

Consumers must pin an immutable source commit. Supply a platform-authorized
local gRPC channel to the generated ClientServiceClient. These bindings do not
implement Windows named-pipe dialing, Unix peer credentials or a mobile bridge.
The Go Connect handler supports the gRPC protocol used by this Dart client.
No HTTP Connect wrapper or production transport adapter is included.

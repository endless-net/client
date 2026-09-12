# Accepted Client Protobuf baseline

`client.binpb` is the accepted descriptor for `client.v0`, established on
2026-09-12 from `proto/client/v0`. It is a contract baseline, not evidence of
runtime implementation or release acceptance.

The Protobuf CI job requires this exact file and compares the current schema
with it using Buf's `FILE` breaking rules. Missing or malformed baseline data
fails the job. Do not refresh it automatically on ordinary schema changes.

After an explicitly accepted hard-cutover decision, generate the replacement:

```sh
buf build proto -o contracts/proto-baseline/client.binpb
```

Review the source schema and baseline together. Record the decision here when
replacing the baseline. Changing a version requires explicit authorization.

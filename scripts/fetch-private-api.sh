#!/usr/bin/env bash
# Trusted build only. No token is written to Git config, URLs, helper or artifacts.
set -euo pipefail
[[ -n ${ENDLESSNET_API_READ_TOKEN:-} ]] || { echo 'Dedicated producer API read-only credential required' >&2; exit 1; }
[[ ${GITHUB_EVENT_NAME:-} != pull_request_target ]] || { echo 'Untrusted checkout context denied' >&2; exit 1; }
helper=$(mktemp "${TMPDIR:-/tmp}/client-api-askpass.XXXXXXXX")
cleanup() { rm -f -- "$helper"; }
trap cleanup EXIT
chmod 700 "$helper"
cat > "$helper" <<'ASKPASS'
#!/usr/bin/env bash
set -eu
case "${1% }" in
  "Username for 'https://github.com':") printf '%s\n' x-access-token ;;
  "Password for 'https://x-access-token@github.com':") printf '%s\n' "$ENDLESSNET_API_READ_TOKEN" ;;
  *) exit 1 ;;
esac
ASKPASS
export GIT_TERMINAL_PROMPT=0 GIT_ASKPASS="$helper"
export GIT_CONFIG_COUNT=3 GIT_CONFIG_KEY_0=credential.helper GIT_CONFIG_VALUE_0=
export GIT_CONFIG_KEY_1=core.askPass GIT_CONFIG_VALUE_1="$helper"
export GIT_CONFIG_KEY_2=credential.useHttpPath GIT_CONFIG_VALUE_2=false
export GOPRIVATE='github.com/endless-net/*' GONOPROXY='github.com/endless-net/*' GONOSUMDB='github.com/endless-net/*' GOWORK=off
go mod download all

#!/bin/sh
# EndlessNet: Ubuntu installation through the existing APT repository.
set -eu
export LC_ALL=C

fail() { printf '%s\n' "$*" >&2; exit 1; }
[ "$#" -eq 0 ] || fail 'Использование: sudo sh install.sh'
[ "$(id -u)" -eq 0 ] || fail 'Запустите скрипт через sudo.'
. /etc/os-release
[ "$ID" = ubuntu ] && [ "$VERSION_ID" = 26.04 ] || fail 'Поддерживается Ubuntu 26.04 LTS.'
[ "$(dpkg --print-architecture)" = amd64 ] || fail 'Поддерживается архитектура amd64.'
command -v curl >/dev/null 2>&1 || fail 'Сначала установите curl.'

temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
trap 'exit 1' HUP INT TERM
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
  --max-time 30 https://apt.endlessnet.ru/apt/unng.gpg -o "$temporary/endlessnet.gpg"
[ -s "$temporary/endlessnet.gpg" ] || fail 'Не удалось получить ключ APT.'
printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/endlessnet.gpg] https://apt.endlessnet.ru/apt stable main' > "$temporary/endlessnet.list"
install -d -m 0755 /etc/apt/keyrings /etc/apt/sources.list.d
install -m 0644 "$temporary/endlessnet.gpg" /etc/apt/keyrings/endlessnet.gpg
install -m 0644 "$temporary/endlessnet.list" /etc/apt/sources.list.d/endlessnet.list
apt-get -o APT::Update::Error-Mode=any update
apt-get install -y endlessnet-client
script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "$0" ] && [ -f "$script_directory/endlessnet" ]; then
  command_source="$script_directory/endlessnet"
else
  command_source="$temporary/endlessnet"
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    --max-time 30 https://raw.githubusercontent.com/endless-net/client/main/scripts/endlessnet -o "$command_source"
fi
[ -s "$command_source" ] || fail 'Не удалось получить команду endlessnet.'
install -d -m 0755 /usr/local/bin
install -m 0755 "$command_source" /usr/local/bin/endlessnet
printf '%s\n' 'Client установлен через APT. Запуском службы управляет установленный пакет.'
printf '%s\n' 'Проверка обновлений: sudo endlessnet check-updates'

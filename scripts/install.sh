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
policy=$(apt-cache policy endlessnet-client) || fail 'Не удалось прочитать доступную версию из APT.'
candidate=$(printf '%s\n' "$policy" | awk '$1 == "Candidate:" { print $2 }')
[ -n "$candidate" ] && [ "$candidate" != '(none)' ] || fail 'В APT нет пакета EndlessNet Client.'
metadata=$(apt-cache show "endlessnet-client=$candidate") || fail 'Не удалось проверить пакет EndlessNet Client.'
printf '%s\n' "$metadata" | grep -qx 'Provides: endlessnet-client-linux-runtime' ||
  fail 'В APT пока нет выпуска с рабочим Linux runtime.'
apt-get install -y "endlessnet-client=$candidate"
printf '%s\n' 'Client установлен через APT. Запуском службы управляет установленный пакет.'
printf '%s\n' 'Проверка обновлений: sudo endlessnet check-updates'

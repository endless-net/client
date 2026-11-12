# Client API: контрактный candidate

## Current enrollment lookup — candidate api/v0.5.0

003/FR-007/013/018: существующий `GetEnrollmentAttemptRequest.selector`
расширен `bool current = 4`. Только `current=true` означает чтение текущей
попытки заданного профиля; пустой `profile` означает активный профиль.
Возвращается та же безопасная `EnrollmentAttempt`, в том числе terminal/uncertain
outcome, без keys/native grants или новой mutation. Отсутствие попытки — NotFound;
caller обязан быть текущим OS owner, чужой caller — PermissionDenied. Query не
claim installation: initial ownership по-прежнему разрешена только Enroll/CreateProfile.
`current=false` и отсутствие decoded selector означают InvalidArgument.

Ровно один selector относится к decoded typed oneof/object. Для нескольких
selector fields в binary wire применяется стандартный Protobuf last-wins;
strict raw-wire rejection или custom codec не вводятся. Runtime обязан независимо
проверить OS owner/profile, false/missing selector и отсутствие side effects.
API descriptor/serialization tests не доказывают эту runtime авторизацию.

Именно `current` — чистый snapshot: без Store.Update/expiry mutation, worker
wake, remote I/O или claim. Existing lookup по request_id/attempt_id сохраняет
свою прежнюю семантику; его reconciliation нельзя автоматически перенести на
новую current-ветку. Это обязательная проверка при runtime adoption.

`EnrollmentAttempt.mode = 18` и `hostname = 19` возвращают неизменные исходные
значения Enroll в owner-only projection. После current read CLI может при
необходимости повторить исходный Enroll с теми же request_id/mode/hostname,
не угадывая workstation и не читая private store/journal. Это read-only данные,
не новый способ изменения режима/имени. Unspecified/unsupported mode или пустое
hostname в новой projection запрещают up recovery с явной ошибкой, без fallback
к предположенным значениям или создания нового request_id. Runtime обязан
сохранять exact retry binding; Go/Dart serialization этого enforcement не доказывает.

Это позволяет новой CLI консоли после потерянного Enroll ACK найти исходную
daemon attempt без собственного file journal и повторного Enroll с новым UUID.
Client использует native login EndlessNet/Identity и не знает выбранный провайдер;
упоминание обязательного VK удалено из Enroll schema comments.

Baseline — опубликованный API4 `c1872645fd91dc8a48cf98f2b843b46b82a1a285`.
Go/Dart SDK, `client.binpb`, `generated.sha256` и профиль в `toolchain.json`
перегенерированы локальными pinned tools; поля request_id/attempt_id сохранены.
Consumer handler/CLI, runtime pins и build digest не меняются до публикации
immutable API0.5.0 и явной adoption; соседний api checkout не заменяет published pin.


## Дополнение007 — transport profile SDK и IPC, api/v0.4.0

Пакет `transport` владеет декларативными Profile/Capabilities/Control v1 и
[формальной JSON schema](transport/schemas/transport.schema.json). Profile не
содержит endpoints, grant, script/raw UAPI/CPS или криптографического кода.
Принимаются встроенные `wireguard/1` и `amneziawg/3.1`; новые обязательные
возможности требуют обновления binary. Prelude содержит только literal bytes
и bounded CSPRNG counts с задержкой внутри исходного probe budget. AWG fields —
whitelist headers/padding/header protection/content padding/flags. Таймеры
жизни криптографических ключей сохраняют defaults закреплённого движка.

DecodeProfile/DecodeCapabilities/DecodeControl отвергают duplicate/unknown/
case-alias/null/missing fields, некорректную base64 и выход за bounds до исполнения.
CanonicalProfile/ProfileDigest связывают все параметры, ID/revision и порядок
prelude, сохраняя uint64 без float. ValidateProfiles ограничивает ≤8 профилей
на peer и уникальность IDs. Дополнительные условия Go SDK сверх JSON shape:
непересекающиеся header ranges, порядок min/max, суммарные задержки/байты,
лексикографическая роль proposer, различные identities и точное отражение
bindings в MatchesReply. Это не проверка подписи, текущих прав, replay или пути.

Profile≤64KiB; до16 prelude datagrams≤1200B; sum delays≤3000ms.
Весь probe candidate ограничивается64KiB/cycle в runtime, включая crypto
handshake и ответы; этот счётчик не заменяется одной лишь валидацией профиля.
HeaderProtectionKey и literal templates — protected material, не UI/diagnostics.
Получивший профиль обязан проверить текущую signed pair-bound Coordinator map.

IPC Status.peer_transports и Peer.transport содержат только безопасную
PeerTransportStatus: фактический путь, engine/protocol, profile ID/revision,
search state и finite reason key. Relay+SEARCHING не означает Disconnected;
ни handshake, ни selected transport не доказывают успех приложения. Private
profile parameters/key/nonce/network fingerprint в IPC не экспортируются.

Go/Dart SDK/descriptor сгенерированы pinned pipeline в двух идентичных проходах.
Для adoption требуются успешный main CI, API release и exact published fetch.
Root runtime закрепляет новый SDK отдельной зависимой поставкой; наличие
контракта не подтверждает runtime adoption. Consumer не использует local
replace/GOWORK/копию DTO.

## Контракт006 — удалённый отзыв, api/v0.3.0

`ListNodeRevocationTargets`, `RevokeNodeAccess`, `GetNodeRevocation` доступны только
существующему IPC owner. Список ограничен Network активного профиля; display name
не заменяет Account/Network/Node и Network revision. UI сохраняет command UUID
до вызова; Client обязан сохранить исходную caller/target привязку до dispatch.
Повтор и reopen используют тот же command, изменение входа конфликтует.

`NodeRevocationResult` различает unknown и accepted. Исходные accepted_at,
enforce_by и committed revision присутствуют только при подтверждённом receipt.
Срок — 300 секунд от авторитетного принятия; явный отзыв применяется сразу.
Истечение срока не доказывает остановку трафика. Источника traffic evidence
в MVP006 нет; IPC не экспортирует подтверждённую локальную очистку, credentials
или private keys. DTO/serialization tests не подтверждают OS caller authority,
серверный commit или сетевой результат. Consumers ждут published exact ref.

## Формальный контракт 003 — api/v0.2.0

Enroll теперь принимает сохраняемый request UUID и возвращает EnrollmentAttempt.
Добавлены GetEnrollmentAttempt/ListEnrollmentTargets/SelectEnrollmentTarget/
CancelEnrollmentAttempt; WaitBrowserEnrollment и прежний EnrollmentResult удалены
hard cutover. Provider user_code остаётся только в отдельном renewal-контракте,
который не входит в 003. Схемы, Go gRPC/Connect и Dart SDK сгенерированы локально;
новая поверхность содержит 42 RPC. Новый attempt различает phase/certainty,
выбор контекста, сроки и публичный browser action без grants/private material.
Current schema/descriptor digest обязателен для IPC pair; целевой выпуск нового API — api/v0.2.0.
Consumer Client UI сохраняет прежний published pin; local/path replace не вводится.
Ниже сохранён baseline 001; прежнее direct-result описание не задаёт форму Enroll 003.

## Новая MVP-линейка v0

До отдельно объявленного стабильного major новой линейки обратная совместимость
НЕ поддерживается, включая patch. Все изменения — hard cutover без shims,
dual API и fallback ради прошлых версий. Данные сохраняются прямыми миграциями.
API выпускается независимо от runtime обычным тегом `api/v0.MINOR.PATCH`;
consumer закрепляет точную опубликованную версию без first-party local `replace`
и обхода через GOWORK. Wire package identifiers не задают SemVer модуля.
Короткий CI включает unit/static checks, без integration/E2E и Buf breaking.
Исторические сравнения ниже — provenance, а не обязательство совместимости.

Producer — Client, RB-client. Требования и весь проект описаны в
[единой спецификации workspace](../../../specs/001-endlessnet-project/spec.md).
Это самостоятельный `github.com/endless-net/client/api` Go-модуль и
producer-owned Dart-пакет [dart/](dart/). Бизнес handlers, OS adapter и runtime
в модуль не включены. Тестовые handlers существуют только в `_test.go`.

## Схема и версии

Исходники: [proto/client/v0/](proto/client/v0/). Исторический источник —
[7f80d7c4ee4419349c1922346ff0a61b834a9fe0](https://github.com/endless-net/client/tree/7f80d7c4ee4419349c1922346ff0a61b834a9fe0/proto/client/v0).
Сохраняются package `client.v0`, номера полей и direct-result draft этого
revision. Новый Go import — `github.com/endless-net/client/api/client/v0`;
Connect bindings — `client/v0/clientv0connect`, gRPC bindings — `client/v0`.
Dart import — `package:endlessnet_client_api/client_api.dart`, Git package
path `api/dart`. Генерация и schemas всегда находятся у Client, не UI.

Базовая контрактная версия новой API-линии — `api/v0.1.0`. API tags независимы
от IPC v0 и версии runtime; выпуск выполняет цикл 002 после CI. Предыдущей
поддерживаемой опубликованной `api/` baseline нет; release breaking check
первого выпуска явно не имеет такой базы. [migration.json](migration.json)
фиксирует отдельное сравнение с историей: 4 изменения Go package против
последней схемы и 103 findings против accepted v0 descriptor. Старые generated
bindings содержали generic operations; candidate direct results нельзя объявлять
их совместимым обновлением. EDGE-Q01 требует явного решения о cutover.
Нормализация import path подтверждает сохранение остальных последних wire
shapes, но не отменяет эти breaking findings.

## Локальная генерация

Exact pins и provenance: [toolchain.json](toolchain.json), включая dependency
lock локального Dart generator. Buf и plugins устанавливаются стандартными
механизмами вне репозитория; BSR и remote plugins не используются. Минимальный
Go — 1.26.0 из реальных runtime dependencies, без копирования исторического Go1.27.
Generator запускается только Dart 3.13.5; его tooling package требует SDK
`>=3.13.0 <4.0.0` из-за закреплённого dependency graph. Ограничение runtime
Dart-пакета `>=3.8.0 <4.0.0` задаётся отдельно и не обещает generation на Dart3.8.

Из этого каталога после установки Buf1.50.0, protoc-gen-go1.36.10,
protoc-gen-go-grpc1.5.1 и protoc-gen-connect-go1.19.1:

```bash
(cd tooling/dart && dart pub get --enforce-lockfile)
buf format --diff --exit-code
buf build
buf lint
buf generate
buf build --as-file-descriptor-set -o client.binpb
goimports -w .
(cd dart && dart pub get --enforce-lockfile && dart analyze --fatal-infos)
go build ./...
go vet ./...
go test -short ./...
```

Goimports pipeline использует exact `v0.48.0`. Для проверки воспроизводимости
повторяются `buf generate`, descriptor build и goimports; сравниваются SHA256
всех `.pb.go`, `_grpc.pb.go`, `.connect.go`, Dart `src/gen/` и `client.binpb`.
Lockfiles — входы, а не обновляемые при каждой генерации зависимости.

## Transport и conformance

Выбран gRPC wire over HTTP/2. SDK содержит Go grpc и Connect generated bindings;
проверенная server pair — Connect1.19.1 gRPC handler с grpc-Go1.83.2 и Dart grpc5.1
clients. Это соответствует историческому `connect.WithGRPC()` Client adapter.
Desktop production использует OS-authenticated Unix socket/Windows pipe,
проверяя UID/SID и caller role. Test fixture не осуществляет OS-authorization.
Windows wire smoke использует случайный loopback port только внутри теста;
Unix branch использует временный local socket. Публичный TCP listener runtime
этим не разрешается. Mobile bridge и peer-auth acceptance каждой ОС — отдельные
runtime gates.

```bash
(cd dart && dart run test/transport_smoke.dart)
ENDLESSNET_DART_SMOKE=1 go test -count=1 -run TestGoDartLocalGRPC -v ./client/v0
```

`ENDLESSNET_DART_BINARY` позволяет задать настоящий исполняемый Dart (например,
`dart.exe` вместо Windows batch launcher). Без `ENDLESSNET_DART_SMOKE=1` Go
тест явно skipped: обычный Go test не утверждает Dart transport pass.

Smoke проверяет реальный Protobuf cross-language vector, exact descriptor digest
bootstrap, generated grpc-Go unary client, Dart unary, typed Failure detail и
stream snapshot/order. Fixture сначала подтверждает inbound `ctx.Deadline`,
затем дожидается реального истечения короткого server context с
`context.DeadlineExceeded`, пока более длинный caller context ещё действует.
Dart принимает только полученный server outcome с точным fixture marker;
собственный таймер клиента не может подтвердить этот случай. Explicit stream
cancel подтверждается отдельным `GetRuntimeInfo` probe по test-only metadata
до закрытия канала и до stream timeout. Эти metadata не входят в production IPC.
Serialization tests также проверяют explicit false/absence,
malformed wire, unknown bytes preservation, 42 access annotations и отсутствие
private-key export fields. Это не доказывает runtime validation и domain authorization.

Успешный opt-in тест печатает `CONFORMANCE_PASS` с подтверждениями
`deadline_forwarded=true server_expired=true cancellation_ack=true`.
`ENDLESSNET_CONFORMANCE_MUTATION` предназначен только для тестового fixture:

| Значение | Изменение теста | Требуемый отрицательный результат |
| --- | --- | --- |
| `drop-timeout` | Перед handler удаляется `Grpc-Timeout` | nonzero exit и `CONFORMANCE_MISSING_DEADLINE` |
| `omit-cancel` | Dart пропускает explicit cancel | nonzero exit и `CONFORMANCE_CANCEL_NOT_OBSERVED` |

```bash
ENDLESSNET_DART_SMOKE=1 ENDLESSNET_CONFORMANCE_MUTATION=drop-timeout go test -count=1 -run TestGoDartLocalGRPC -v ./client/v0
ENDLESSNET_DART_SMOKE=1 ENDLESSNET_CONFORMANCE_MUTATION=omit-cancel go test -count=1 -run TestGoDartLocalGRPC -v ./client/v0
```

Обе mutation-команды обязаны завершиться ошибкой с указанным marker. Ошибка
компиляции или подготовки dependencies без marker не является пойманной mutation.

Неподдержанная в проведённом эксперименте pair: generic grpc-Go1.76/1.83.2 server
с Dart grpc5.1/http2 2.3.1 при server deadline может вернуть HTTP2 RST CANCEL,
который Dart раскрывает как UNKNOWN вместо DeadlineExceeded. Primary mechanism:
grpc-Go `internal/transport/http2_server.go` deadline timer `closeStream` и
Dart `lib/src/client/call.dart` error mapping. Проверка не заменена synthetic
успехом: supported fixture использует выбранный выше Connect handler. Изменение
server implementation требует повторить actual expiration тест, не ослабляя assertion.

## Потребление Flutter

Для UI dependency использовать Git URL `https://github.com/endless-net/client.git`,
exact immutable tag `api/v0.1.0` и package path `api/dart` после успешного tag pipeline
002. До появления тега не подменять этот pin соседним checkout или fixture revision.
Локальная приёмка создаёт отдельный временный Git candidate repository,
фиксирует его exact revision и импортирует Dart SDK минимальным Flutter consumer.
Этот fixture revision не является producer commit, remote pin или release.
Flutter import не доказывает production UI adoption или мобильную реализацию.

Dependency security gate: `golang.org/x/net` закреплён на `v0.60.0` после
findings GO-2026-6617/6612/6611/6603 в `v0.58.0`. Транзитивные
`x/sys v0.48.0` и `x/text v0.42.0` и minimum Go1.26 согласованы в go.mod.
Повторные результаты обязательного vulnerability scan фиксируются workspace.

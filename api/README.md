# Client API: контрактный candidate

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

Это новая неопубликованная API-линия. Будущие tags `api/v<semver>` независимы
от IPC v0 и версии runtime. Номер публикации сейчас не назначен. Предыдущей
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
malformed wire, unknown bytes preservation, 39 access annotations и отсутствие
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

Для будущего UI dependency задаются Git URL Client, exact immutable revision
с этими файлами и path `api/dart`. Такой remote revision пока не опубликован.
Локальная приёмка создаёт отдельный временный Git candidate repository,
фиксирует его exact revision и импортирует Dart SDK минимальным Flutter consumer.
Этот fixture revision не является producer commit, remote pin или release.
Flutter import не доказывает production UI adoption или мобильную реализацию.

Dependency security gate: `golang.org/x/net` закреплён на `v0.60.0` после
findings GO-2026-6617/6612/6611/6603 в `v0.58.0`. Транзитивные
`x/sys v0.48.0` и `x/text v0.42.0` и minimum Go1.26 согласованы в go.mod.
Повторные результаты обязательного vulnerability scan фиксируются workspace.

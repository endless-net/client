# Client Mobile Bridge: контракт адаптера и mock

- Owner: `client`; consumer: `client-ui` (Android/iOS).
- Status: **published contract for adapter implementation and UI mocks**.
- Дата: 2026-09-14. Native mobile bridge/runtime ещё не реализован и не принят.
- Бизнес-контракт: **client.v0**, без изменения schema, descriptor или версии.

Это нормативный логический интерфейс между UI и native adapter. По нему
`client-ui` реализует одинаковый интерфейс для mock и будущего platform adapter.
Псевдокод ниже реализован как импортируемые Dart interfaces и result types в
[mobile_bridge.dart](../packages/client_api/lib/src/mobile_bridge.dart),
экспортируемые через `package:endlessnet_client_api/client_api.dart`.
`MobileBridge` и `MobileChannel` реализуются consumer mock и будущим adapter.
Go/Kotlin/Swift реализации и native транспорт эта публикация не предоставляет.
Документ не определяет native ABI, Binder framing или iOS provider-message wire.

## 1. Источники и подключение consumer

Канонические [RPC и сообщения](../proto/client/v0/service.proto),
[общие типы](../proto/client/v0/common.proto),
[runtime state](../proto/client/v0/runtime.proto) и
[семантика IPC](client-ipc-protobuf.md) имеют приоритет в бизнес-семантике.
Этот документ определяет только binding. Реализации
[metadata/access guard](../clientipc/rpc/protocol.go),
[runtime boundary](../internal/client/service_rpc_handlers.go) и
[очереди событий](../internal/client/service_rpc_events.go) служат основанием
для описанных правил, но не доказывают мобильную поддержку.

Для передачи команде UI используйте permalink вида
`https://github.com/endless-net/client/blob/<commit-sha>/docs/client-mobile-bridge.md`.
Зафиксируйте тот же commit для [Dart SDK](../packages/client_api/README.md)
и его `ClientContract`, а также для спецификации и mock fixtures. Ссылка `main`
подходит для навигации, но не фиксирует тестовый контракт. Отдельный номер
версии bridge не вводится; descriptor digest описывает Protobuf, а commit
фиксирует также эти правила адаптера.

Mock сериализует и декодирует настоящие generated Protobuf messages, например
через Dart `writeToBuffer` / `fromBuffer`. JSON-модель бизнес-сообщений не нужна.
Generated `ClientServiceClient` требует gRPC channel: данный логический adapter
не является готовой реализацией такого channel. UI может реализовать facade
поверх generated messages и этого API. Пример использования импортируемого
интерфейса находится в [Dart README](../packages/client_api/README.md).
Готовый native bridge и сценарный UI mock остаются отдельными реализациями.

## 2. Логический API

Псевдокод задаёт значения и асинхронную семантику; синтаксис языка свободен.
В Dart имена типов имеют префикс `Mobile` (`MobileRequest`, `MobileRpcError`,
`MobileSubscription` и т. д.); enum members используют lowerCamelCase.
Например, `CHANNEL_LOST` соответствует `MobileBridgeCode.channelLost`.
RPC enum содержит стандартные non-OK statuses; его ordinal не является wire code.
`Bytes` — отдельное Protobuf message без HTTP/gRPC frame или Base64 оболочки.
Переданные буферы считаются неизменяемыми до завершения вызова; adapter копирует
их, если native boundary не обеспечивает такое владение.

```text
Metadata = list of (lowercase_name: String, value: String)
Request = { procedure: String, metadata: Metadata, payload: Bytes }

RpcError = { status: RpcStatus, failure: Bytes? }
BridgeError = { code: BridgeCode }
UnaryResult = Success(payload: Bytes) | RpcError | BridgeError
StreamEnd = Completed | RpcError | BridgeError
OpenResult = Opened(channel: Channel) | BridgeError

OpenAttempt = { result: Future<OpenResult>, cancel(): void }
Call = { id: OpaqueId, result: Future<UnaryResult>, cancel(): void }
Subscription = { id: OpaqueId, ended: Future<StreamEnd>, cancel(): void }

open(timeoutMillis: positive integer): OpenAttempt
Channel.unary(request: Request, timeoutMillis: positive integer): Call
Channel.subscribe(request: Request,
                  lifetimeMillis: positive integer or absent,
                  onEvent: (Bytes) -> Future<void>): Subscription
Channel.close(): Future<void>
```

- `open` обращается к фиксированному native host установки. UI не передаёт
  endpoint, путь к state, identity или роль. `Opened` означает доступный канал,
  а не успешный bootstrap, enrollment или VPN connection. Открытие не является
  командой Connect и не показывает системный permission prompt автоматически.
- `procedure` — точное имя `/client.v0.ClientService/<Method>` из descriptor.
  `unary` используется для unary RPC; `subscribe` — для `WatchEvents`, единственного
  server-streaming RPC текущего v0. Неизвестное имя или неверный вид вызова
  отклоняется локально как `BridgeError(INVALID_ARGUMENT)` без dispatch.
- Payload должен соответствовать input message метода, результат — его output
  message. Protobuf не содержит идентификатор типа: проверки wire decoding не
  доказывают правильность выбранного типа. Consumer использует generated types,
  producer сохраняет проверки полей и domain preconditions.
- Методы немедленно возвращают handle; results, events и completion доставляются
  асинхронно, после возврата handle. Вызовы могут завершаться в любом порядке.
  `id` уникален в пределах channel и не переиспользуется в его lifetime.
- Timeout измеряется монотонным временем от вызова API, включая локальное
  ожидание. Отсутствующий `lifetimeMillis` означает подписку до terminal,
  отмены или закрытия. При заданном lifetime учитывается весь stream, а не
  отдельные события; idle timeout по умолчанию не добавляется.
- `cancel` идемпотентен и завершает ещё незавершённое ожидание с
  `BridgeError(CANCELLED)`. Уже зафиксированный terminal не меняется.
  Гонки cancel/deadline/result разрешаются первым terminal в сериализованном
  состоянии adapter; ровно один terminal виден consumer.
- `close` идемпотентен: переводит channel в закрытое состояние, отменяет локальные
  ресурсы и завершает незавершённые handles с `BridgeError(CHANNEL_CLOSED)`.
  Последующие вызовы на нём получают тот же код. Закрытый channel не открывается
  повторно: нужен новый `open`. Закрытие не отправляет Disconnect, Logout или
  `NotifyLifecycle` и не владеет lifetime VPN.
- Разрыв native связи закрывает channel; незавершённые handles получают
  `BridgeError(CHANNEL_LOST)`. Автоматических re-open, retry или re-subscribe
  внутри adapter нет. UI выполняет явное восстановление из раздела 5.

Transport `Call.id` / `Subscription.id` никогда не подставляется в
`MutationContext.request_id`. Отмена ожидания, timeout и закрытие UI не отменяют
принятую runtime операцию. Adapter не повторяет мутации и не создаёт для них
новые request IDs.

## 3. Bootstrap, metadata и identity

После `open` UI вызывает `GetRuntimeInfo` с `GetRuntimeInfoRequest {}`.
Только этот RPC может не иметь contract metadata; он всё равно требует
аутентификацию. UI проверяет `runtime.protocol == "endlessnet-client-ipc"`,
`runtime.ipc_version == 0`, точное совпадение `runtime.contract_sha256` с
`ClientContract` закреплённого SDK и непустой `runtime.instance_id`.
Протокол, digest и instance ID не могут быть заменены protobuf defaults.
При несовпадении UI показывает incompatible и не продолжает business RPC.
Adapter не подменяет сведения runtime ожидаемыми значениями.

Последующие запросы передают ровно по одному значению:

```text
x-endlessnet-ipc-protocol: endlessnet-client-ipc
x-endlessnet-ipc-version: 0
x-endlessnet-ipc-contract-sha256: <ClientContract descriptor digest>
```

Список metadata сохраняет дубликаты, чтобы mock мог проверить отказ, а adapter
не маскировал несовместимость. Значения передаются без исправлений и автозаполнения.
Timeout передаётся отдельным аргументом, не новым header контракта. Произвольные
credentials или заявления о роли в metadata не дают полномочий. Runtime
проверяет authorization до metadata: неавторизованный caller не получает вместо
отказа доступа сообщение о совместимости.

Identity устанавливает доверенный native host по проверенному OS/app context.
Observer/owner/administrator, initial ownership claim и redaction остаются
producer-owned по annotations v0. Авторизованный app не становится автоматически
owner или administrator. Mock задаёт trusted caller в тестовой конфигурации,
недоступной через UI API, и воспроизводит соответствующую фильтрацию.

Capabilities читаются из `RuntimeInfo`, отсутствие означает unsupported.
Наличие адаптера не позволяет объявить Android/iOS capabilities доступными.
`UNSUPPORTED`, `POLICY_BLOCKED`, `PERMISSION_REQUIRED` и
`TEMPORARILY_UNAVAILABLE` в `Restriction.availability` различаются.
Мобильный эквивалент administrator требует отдельного authorization дизайна.

## 4. Ответы, ошибки и ограничения

`RpcStatus` — стандартное имя status существующего RPC boundary, например
`PERMISSION_DENIED`, `FAILED_PRECONDITION`, `UNIMPLEMENTED`, `NOT_FOUND`,
`RESOURCE_EXHAUSTED`, `CANCELLED`, `DEADLINE_EXCEEDED` или `INTERNAL`.
В `RpcError` нельзя передавать `OK`. `failure`, если есть, содержит serialized
`client.v0.Failure` из typed RPC detail; это не JSON и не serialized error string.
Adapter сохраняет status и Failure, не выводит domain code из текста ошибки.
При отсутствующем typed detail status сохраняется, `failure` отсутствует:
UI показывает общий исход без выдуманного `Failure`.

`BridgeCode` — закрытый набор локальных исходов этого логического API:

| Code | Значение |
| --- | --- |
| `UNAVAILABLE` | Native host/channel пока недоступен, в том числе до VPN setup |
| `ACCESS_DENIED` | Native boundary отказал в доступе до получения RPC ответа |
| `INVALID_ARGUMENT` | Некорректный аргумент bridge, procedure или cardinality |
| `INVALID_RESPONSE` | Повреждённый ответ, framing или нарушение порядка stream |
| `DEADLINE_EXCEEDED` | Истёк локальный timeout/lifetime |
| `CANCELLED` | Consumer отменил ожидание или callback отклонил событие |
| `CHANNEL_CLOSED` | Channel закрыт consumer |
| `CHANNEL_LOST` | Потеря открытого native канала |
| `LIMIT_EXCEEDED` | Локальный лимит message или очереди превышен |
| `INTERNAL` | Прочая внутренняя ошибка адаптера |

Эти коды не добавляются в Protobuf `ErrorCode`. `BridgeError` не содержит
`Failure`, RPC status или произвольный diagnostic text. Полученный от producer
deadline/limit/access error остаётся `RpcError`, даже если локальный код похож.
Неудачный `open` и неизвестность результата Connect — разные пользовательские
ситуации; транспортный сбой после dispatch не доказывает отсутствие эффекта.

`Success(ConnectResponse)` может содержать pending или failed `Operation`:
RPC success не означает успешную бизнес-операцию. `Operation.failure` и
`WatchEventsResponse.failure` — части обычных Protobuf messages. Последний
не является автоматически terminal stream error.

Размеры считаются по Protobuf bytes без native framing: request максимум
65 536 bytes (64 KiB); unary response или одно событие максимум 4 194 304 bytes
(4 MiB). Превышение, обнаруженное adapter, даёт `BridgeError(LIMIT_EXCEEDED)`;
producer отказ сохраняет RPC status и typed detail. Fragmentation/reassembly
для меньших native transport limits — обязанность будущего platform binding;
он не вправе молча урезать или частично выдавать message.

## 5. WatchEvents и восстановление

Первое событие успешной подписки — `WatchEventsResponse` с `sequence = 1` и
`snapshot { runtime, status }`. До первого snapshot допустим terminal error.
Каждое следующее событие увеличивает sequence ровно на один в этой подписке.
Adapter сохраняет `metadata`, instance ID, revisions и Protobuf payload:
не нумерует события заново и не объединяет их. Проверка нарушенного stream
(не snapshot первым, разрыв sequence, смена instance внутри подписки или
невалидные bytes) завершает подписку `BridgeError(INVALID_RESPONSE)`.
Одинаковая revision у нескольких событий допустима; sequence не является revision.

Для каждой подписки `onEvent` вызывается последовательно. Future от callback
служит подтверждением обработки и создаёт backpressure; callback не должен
блокировать UI thread. В очереди adapter не более 64 ещё не переданных событий
и суммарно 8 MiB Protobuf bytes, плюс максимум одно событие в callback.
Нельзя создавать дополнительную неограниченную очередь на native/UI boundary.
Переполнение закрывает подписку с `BridgeError(LIMIT_EXCEEDED)`, очищает очередь
и отменяет producer subscription; события не пропускаются для продолжения потока.
Producer имеет собственную ограниченную очередь: её переполнение приходит как
`RpcError(RESOURCE_EXHAUSTED, Failure{code: ERROR_CODE_LIMIT_EXCEEDED})`.

Terminal доставляется через `ended` ровно один раз. Ошибка или отмена не ждёт
зависший callback. Чистое завершение producer сначала дожидается обработки уже
принятых событий и только затем выдаёт `Completed`; в этом ожидании по-прежнему
действуют cancel, close и заданный lifetime. Чистый EOF до первого snapshot —
`BridgeError(INVALID_RESPONSE)`, а не `Completed`.
После terminal новые callbacks не начинаются, очередь очищается; уже начатый
callback может закончиться позже, и consumer игнорирует его устаревшее продолжение.
Ошибка Future callback отменяет subscription с `BridgeError(CANCELLED)`.
`close` освобождает transport resources, но не ждёт пользовательский callback.
Producer terminal, включая чистое завершение `Completed`, завершает наблюдение;
он не означает Disconnect или успешную операцию.

После разрыва, resume или terminal, требующего нового наблюдения, UI закрывает
старые handles и выполняет новый `open` (если channel закрыт), bootstrap и
подписку. Первый snapshot заменяет прежнее состояние; старые callbacks
отфильтровываются по channel/subscription identity. Snapshot из `GetStatus`
и первый snapshot stream не образуют непрерывную историю. До нового snapshot
UI не считает прошлое состояние актуальным. Replay cursor отсутствует;
polling `GetStatus` не реализует `WatchEvents`.

`invalidated` требует повторного чтения названного domain, а не delta merge.
Первый owner snapshot содержит `status.current_operations`; observer их не
получает. При потерянном ответе мутации UI использует
`GetOperationRequest { request_id: <исходный MutationContext.request_id> }`.
`NOT_FOUND` не доказывает, что задержанный запрос никогда не будет принят;
UI сохраняет неопределённость и не повторяет Connect автоматически. Правила
retention и durable deduplication остаются в основном IPC контракте.

## 6. Сценарии для consumer mock

Нотация `PB(Type { fields })` означает serialization generated message.
Это символические fixtures, не новый wire format. Для сокращения ниже опущены
полные имена procedure и обязательные поля вложенных объектов: mock заполняет
их согласованно с v0 (UUID, metadata, operation kind, terminal outcome).
Нельзя принимать сокращённую запись за готовый валидный request.

### Bootstrap и наблюдение

```text
open -> Opened(C1)
C1.unary(GetRuntimeInfo, PB(GetRuntimeInfoRequest {}), metadata=[])
  -> Success(PB(GetRuntimeInfoResponse { runtime: R1 }))
UI validates R1 protocol/version/digest/instance_id
C1.unary(GetStatus, PB(GetStatusRequest {}), metadata=M)
  -> Success(PB(GetStatusResponse { status: S1 }))
C1.subscribe(WatchEvents, PB(WatchEventsRequest {}), metadata=M)
  -> onEvent(PB(WatchEventsResponse {
       sequence: 1, metadata: K2, snapshot: { runtime: R1, status: S2 }
     }))
```

`M` — три точных metadata значения; `K2` соответствует `S2.metadata` и
instance `R1`. `S2` может быть новее `S1`: UI заменяет состояние, не теряет
показанные в `S2.current_operations` операции и не посылает Connect при attach.

### Connect и потерянный ответ

Предусловия: caller owner, выбран активный enrolled/approved профиль `P`,
capability connection доступна, получены свежие instance `I` и revision `V`.
Для чистой установки mock сначала воспроизводит initial claim/profile/enrollment;
bootstrap сам по себе не разрешает Connect.

```text
unary(Connect, PB(ConnectRequest {
  mutation: { request_id: Q, expected_instance_id: I, expected_revision: V },
  profile: { profile_id: P }
}), metadata=M) -> Success(PB(ConnectResponse { operation: O_pending }))
onEvent(WatchEventsResponse { sequence: next, operation_changed: O_running, ... })
onEvent(WatchEventsResponse { sequence: next, operation_changed: O_succeeded, ... })
onEvent(WatchEventsResponse { sequence: next, status_changed: S_connected, ... })
```

Это один допустимый scripted порядок, не обязательный порядок operation/status
для всех runtime. Промежуточные состояния могут не наблюдаться. `O_*` сохраняют
одинаковые `id`, `request_id = Q`, `profile_id = P`,
`kind = OPERATION_KIND_CONNECT`; pending/running не имеют outcome, succeeded
имеет предусмотренный v0 success outcome. `S_connected.connection_phase` равен
`CONNECTION_PHASE_CONNECTED`. UI не показывает Connected только по RPC success;
успешный доступ к реальному ресурсу отдельно проверяется platform acceptance.

В варианте lost response mock принимает Q, но завершает Call ошибкой
`CHANNEL_LOST`. После нового open/bootstrap/snapshot UI вызывает
`GetOperationRequest { request_id: Q }`, получает ту же операцию и не отправляет
второй Connect. Варианты: операция pending, succeeded, failed и `NOT_FOUND`.
Новый transport Call.id не меняет Q; restart runtime меняет instance ID,
но не создаёт новую операцию вместо принятой.

### Ошибки, ограничения и lifecycle

| Fixture | Ожидаемое поведение UI/mock |
| --- | --- |
| Bootstrap возвращает другой digest/protocol/version | UI показывает incompatible; дальнейших business RPC нет |
| Авторизованный request с неверным или повторным M header | `RpcError(FAILED_PRECONDITION, PB(Failure { code: ERROR_CODE_CONTRACT_MISMATCH }))`; мутация не принята |
| Observer вызывает owner method | `RpcError(PERMISSION_DENIED, PB(Failure { code: ERROR_CODE_OWNER_REQUIRED }))`; нет owner данных |
| Capability отсутствует или unsupported | UI отключает соответствующее действие; принудительный вызов fixture получает `RpcError(UNIMPLEMENTED, PB(Failure { code: ERROR_CODE_UNSUPPORTED }))` |
| Policy blocked / VPN permission required | Раздельные v0 restriction/Failure сценарии; permission не превращается в success или unsupported |
| Host недоступен до настройки VPN | `open -> BridgeError(UNAVAILABLE)`; ни fabricated snapshot, ни автоматического Connect |
| Cancel/deadline после принятия Q | Один transport terminal; Q остаётся доступен через GetOperation |
| Закрытие C1, поздний callback, открытие C2 | Старый callback не меняет новое состояние; C2 stream начинается с sequence 1 и нового snapshot |
| `subscription.cancel()` дважды | Один `ended = BridgeError(CANCELLED)`, нет новых callbacks или Disconnect |
| Callback не завершён, 64 события уже в очереди, приходит ещё одно | Один `BridgeError(LIMIT_EXCEEDED)` без ожидания callback и без дальнейшего stream delivery |
| Очередь превышает 8 MiB до достижения 64 событий | Тот же bounded overflow исход; request/response проверяются также на своих границах |
| Producer завершает stream ошибкой после событий | Полученные status и typed Failure сохраняются в `ended`; последний status не считается свежим после потери наблюдения |
| `WatchEventsResponse.failure` без terminal | Обычное событие; следующие события разрешены |
| Stream завершён чисто / callback Future rejected | Соответственно `Completed` / `BridgeError(CANCELLED)`; это не VPN Disconnect |
| Первым пришёл delta, пропущен sequence или повреждён payload | `BridgeError(INVALID_RESPONSE)`; UI требует новое наблюдение |

Mock должен иметь управляемые тестом clock, completion gates и event delivery,
чтобы проверять эти гонки без wall-clock sleeps. Неожиданный business вызов
считается ошибкой теста, а не автоматически успешным ответом. Сравнение requests
выполняется по decoded Protobuf значениям, а не по порядку байтов сериализации.
Тесты не выводят enrollment tokens, browser URLs или произвольные payloads в logs.

## 7. Граница реализации и acceptance

Обязательства mock: API lifecycle, точные сообщения и metadata, разграничение
ошибок, scripted caller filtering, snapshot-first stream, bounds, cancellation
и отсутствие неявных повторов. Это позволяет проверить UI reducers, recovery
и доступность действий без устройства или native host.

Mock не доказывает OS identity, реальную authorisation/redaction producer,
durable acceptance, VPN traffic, background lifecycle, signing, permissions
или работу опубликованной пары UI/core. Для этого нужны отдельные producer,
native и same-manifest platform tests.

Android Binder/JNI/gomobile, размещение control service и iOS provider messages
будут конкретными реализациями под этим интерфейсом. Они должны отдельно решить
message framing/fragmentation, доставку ordered stream и platform identity.
Одноразовый iOS provider message сам по себе не реализует подписку.
Bootstrap до запуска VPN и continuation исходного Connect после OS permission
остаются задачами runtime host. Этот контракт не добавляет permission-result RPC
и не разрешает UI повторить Connect для имитации continuation.

После реализации platform adapter должен пройти те же consumer contract cases,
а затем проверки реального host и ресурса. До этого публикация спецификации
не меняет mobile support/capability/release acceptance status.

# client

## Connectivity005 — локальное состояние

Protected state format2 хранит last Connect/Disconnect intent, поколения и журнал
собственных сетевых ресурсов отдельно от регистрации. Обычный Windows startup
прямо мигрирует format1→2 под тем же DPAPI/installation binding, затем читает
только текущий формат. Ключи, owner, original enrollment и credentials сохраняются.
Current state не перезаписывается; missing/corrupt/unknown state не reset.

Журнал содержит prepared/applied/removing adapter/address/route/filter records;
Disconnect сохраняет незавершённую очистку прежнего поколения. Он не разрешает
трафик и не доказывает применение или отсутствие ресурсов ОС. IPC handlers
Connect/Disconnect сохраняют accepted request(instance/revision), generation и
исходный срок до dispatch. Повтор running/applied intent присоединяется без записи
и продления срока; явный повтор после terminal failure получает новое поколение.
Windows runtime подключает actual factory и обязательный connection worker.
GetStatus связывает сохранённое намерение с current generation worker observation.
Без такого наблюдения показывает unknown application/recovering.

WindowsSession.Restore требует отдельный context с конечным сроком до30s и sealed
map receipt после создания session в том же clock epoch. Старый истёкший Connect
deadline не переносится и не переписывается; cached map/cursor не восстанавливает
authority. Factory связывает cleanup, fresh full fetch и Restore.

`internal/clientapp.ConnectionWorker` реализует daemon lifetime для одного active
profile: последний durable target, join повторов, cancellation прежнего поколения
и bounded startup/Connect/Disconnect. Polling перечитывает intent после потерянного
commit ACK/wake, но не повторяет terminal generation. Следующий owner ждёт старого
cleanup; при ошибке Close исходный owner удерживается для явного retry. Pending
Disconnect имеет собственный срок даже при зависшем старом apply. Caller wait
не владеет session lifetime. Session backend должен подтвердить fresh authority,
local apply и Direct path; unit backend этого worker подставной. Runtime подключает
NewWindowsConnectionFactory и проецирует worker observation в status/events.

Статус различает accepted intent, подтверждённое session apply, Direct transport
evidence и offline existing-only permission. Один listener не даёт CONNECTED.
Native transition возвращает unknown без ожидания SDK mutex; stale profile/
generation observation отбрасывается. Ошибка попытки сохраняется после успешной
очистки, а ошибка очистки отображается отдельно. Категории permission/conflict/
apply/path/control/access/cleanup/clock не содержат raw backend text. Срок node
credential показывается отдельно от user session. CONNECTION capability объявляется
при подключённых runner/observation; selected-profile authority проверяется RPC.

WatchEvents начинает с caller-filtered runtime/status SnapshotEvent, sequence1.
Далее отправляет full status и domain invalidations; профиль инвалидирует его
cached domains, смена bootstrap capability/access выдаёт новый SnapshotEvent.
Sequence строго растёт в пределах stream; reconnect начинает новый snapshot.
Metadata revision остаётся монотонным durable Store CAS: volatile observation
между writes упорядочивается sequence, без искусственных persistent writes.
Sampling interval берётся из CONNECTION_POLL_INTERVAL. Queue64/8MiB включает
in-flight send; overflow завершается typed LIMIT_EXCEEDED, без скрытой потери
уже поставленных событий. Отмена/закрытие stream не меняет accepted intent.
Эти projections не доказывают доступность прикладного ресурса или native drift
acceptance; real OS mutations/packet classification остаются непроверенными.

Direct observation теперь использует pinned wireguard-go callbacks: usable session
keys плюс inbound transport/keepalive после decrypt/replay-check от configured
endpoint. Одно Established/listener не подтверждает путь. Binding tokens отвергают
late callbacks после endpoint change/remove/readd; unannounced endpoint roaming
не принимается. Handshake инициируется один раз, retry timers принадлежат движку;
повторные keepalive требуют current send keypair. Native WaitDirect не держит
session mutex во время ожидания, проверяет current intent/map/budget перед ответом
и оставляет Complete worker. Это transport evidence, без обещания TCP/UDP-ресурса.
Actual crypto/in-memory и local-only UDP fixtures проверены отдельно от native
Wintun/routes/WFP и двух устройств; native packet acceptance не подтверждена.

Factory получает signed full snapshot с сохранением anti-rollback cursor, применяет
native session, проверяет bound port, сохраняет endpoint ACK и ждёт Direct.
Renewal после потерянного ACK повторяет тот же frozen request/generation.
Если recovered ACK истёк или описывает прежний endpoint, до Direct требуется
отдельный next-generation announcement. Один announce ограничен двумя dispatch,
исходный Connect budget не обновляется; uncertainty останавливает цепочку.
Потеря stream или новая карта немедленно закрывают packet gate; старый native completion
не снимает блокировку более нового source epoch. Offline сохраняет только
ограниченные original authority existing flows. Revoke/overflow прекращают доступ.

Все параметры `ENDLESSNET_CLIENT_CONNECTION_` обязательны, без defaults:
`ENDPOINT`, `LISTEN_PORT`, `MTU`, `ROUTE_METRIC`, `MAX_PEERS`, `MAX_RULES`,
`MAX_PROTECTED`, `MAX_INVENTORY_ROWS`, `MAX_INTERFACES`, `MAX_QUEUED_MAPS`,
`MAX_MAP_BYTES`, `MAX_STREAM_RECORDS`, `MAX_CALLS`, `MAX_STREAMS`, `STREAM_DURATION`,
`READ_IDLE_TIMEOUT`, `ENDPOINT_TTL`, `RENEWAL_INTERVAL`, `RETRY_INTERVAL`,
`PROBE_INTERVAL`, `POLL_INTERVAL`, `CLEANUP_TIMEOUT`, `DAD_POLL_INTERVAL`,
`TRANSACTION_WAIT`, `APPLY_TIMEOUT`. ENDPOINT — явно заданный numeric IPv4:port,
не loopback; external port может отличаться от LISTEN_PORT без автоматического
NAT discovery. TTL не более300s, renewal раньше expiry, apply не более30s,
cleanup не более10s и меньше STOP_TIMEOUT. Parsing не обращается к сети/ОС.
Один native Clock с epoch runtime UUID используется Controller/Worker/Session.
Fx связывает IPC, enrollment и connection как три обязательных цикла. Ошибка
очистки при остановке не скрывается cancellation и завершает shutdown ошибкой.

Accepted request также сохраняет ClockEpoch и lower/upper uptime receipt. Worker
считает30s/10s от исходного counter, включая задержку до wake и suspend; новый
epoch/небезопасные часы не превращаются в wall-clock fallback. OperationBudget
передаётся в context и обязателен для native Start/Restore. Initial Start сверяет
exact accepted receipt, Lease проверяет бюджет перед TUN/UDP I/O, а final activation
проверяет его ещё раз. Local Start сам не завершает бюджет: до timely Direct
decision остаётся30s предел. Complete не продлевает signed authority; после него
срок карты/offline/revoke продолжают действовать отдельно. Production composition
передаёт общий WindowsClock и ожидает Direct confirmation.

`internal/connectivity` проверяет подписанные full snapshot/resync через producer
SDK: exact device context, current purpose trust, directed ACL и IPv4/Direct
ограничения. Anti-rollback cursor сохраняется под Connect generation/state CAS;
heartbeat/replay не новое подтверждение права. HTTP consumer передаёт только
node credential, обрабатывает bounded NDJSON и exact endpoint ACK; durable
preparation/sent marker/ACK сохраняют исходный запрос при uncertainty. Endpoint
ACK не даёт traffic authority и не продлевается повтором. Daemon factory связывает
refresh/renewal и native apply; отдельный adapter check не означает рабочий tunnel.

Volatile Lease ограничивает authority original receipt/expiry и запрещает новые
flows при недоступном control plane. Revoke/cancel/небезопасные часы прекращают
доступ. Windows clock читает suspend-inclusive interrupt time; restart не
восстанавливает grant. Engine требует Lease и проверяет срок после TUN Read,
перед OS Write и перед UDP Send после шифрования. Dropped packets не ожидают
нового права. Lifetime guard не определяет, какой поток был ранее разрешён.

`internal/dataplane` подключает `tailscale/wireguard-go` к предоставленным TUN/UDP
bind. Peer refresh сохраняет engine sessions, удаляет исчезнувшие peers и не
экспортирует UAPI/private key. Ошибка обновления закрывает движок. Windows adapter,
routes, directed filters и Client lifecycle соединены через WindowsSession;
unit fixture использует TUN/bind без OS handles и не доказывает сетевую связь.

Engine требует explicit nonzero UDP listen port. Initial UAPI задаёт его один
раз, refresh peers не меняет listener. Bind wrapper проверяет exact returned port
и receive functions; ошибка/другой port закрывают engine без random fallback.
BoundPort сообщает только подтверждённый local listener, без IpcGet или keys.
Будущий endpoint worker должен связать его с отдельно заданным externally reachable
Direct endpoint; сам факт bind не означает право или доступность peer.

Windows amd64 packaging включает официальный Wintun0.14.1 `wintun.dll` и лицензию
по exact archive/file SHA256 из `internal/wintundriver/manifest.json`.
`internal/wintundriver.Load` проверяет adjacent DLL до загрузки, удерживает её и
предков пути от замены/rename, запрещает reparse/UNC и ранее загруженный same-name
module. Зависимости DLL ищутся только в System32; непроверенная замена не выбирается.
Upstream DLL logger выключается до adapter calls. Isolated native load/version
проверены без создания адаптера или установки драйвера. WindowsTUNFactory вызывает
Load, проверяет GUID/name conflicts до и после protected preparation, создаёт новый
TUN и передаёт его только после measured GUID/LUID receipt ACK. Повтор existing
journal запрещён; uncertain/late result закрывает исходный creation handle.
ConfirmAbsent отдельно проверяет native inventory; Close не считается очисткой.
Factory подключена через WindowsSession; native adapter mutation пока не проверена.

WindowsCleanup объединяет recovery старого журнала: bounded context, native
adapter absence, removing commit, сверка IP rows, persistent WFP cleanup и retire.
Whole-journal/revision fences сохраняют новую intent и регистрацию. Live/recreated
adapter, leftover row, unknown WFP child или lost ACK оставляют незавершённый
journal; cached permits/keys не восстанавливаются. Live owner должен сначала
отозвать Lease и закрыть engine/TUN/dynamic handles. Recovery подключена
к startup/Disconnect через factory; privileged native deletion пока не проверена.

WindowsSession объединяет initial Start/Stop: verified map/current trust/intent
fences, paused packet I/O, durable cursor/TUN/filter/IP receipts, own-interface
MTU, DAD readiness, exact /32 routes, actual WG engine и confirmed listener.
Dynamic exceptions освобождаются до TUN, затем запускается journal cleanup.
Pause не продлевает Lease; late/uncertain result не активирует доступ. Подписанная
карта и настоящий Store проверены в unit composition с fake native API и inert
TUN/bind. Refresh сохраняет unchanged peer objects/permit IDs, сверяет и меняет
только необходимые /32 routes; старые packet deny scopes остаются до teardown.
Offline устанавливает reauthorization-only policy без нового Confirm/продления
Lease; свежая подписанная карта может возобновить online. Stop прерывает update,
любая uncertainty оставляет cleanup journal и не восстанавливает старый grant.
Status/events wiring выполнено; privileged WFP/flow classification этой
композиции пока не проверены.

Windows WFP adapter готовит own-LUID directed TCP/UDP ALE rules и меняет их через
отдельную dynamic session/write transaction. Offline rules требуют reauthorization;
empty grants запрещают доступ. Ownership mismatch и uncertain commit не дают
успеха или автоматического повтора. Adapter подключён через WindowsSession/journal;
unit/ABI fixtures не подтверждают фактическую фильтрацию ОС.

IP Helper adapter готовит отдельный local /32 и peer /32 routes, проверяет conflicts
до Create и GUID/LUID/actual proof перед Delete. DAD readiness отделена от receipt
создания. Intrinsic local route receipt сохраняется с address record; позднее
наблюдение служит cleanup metadata. Cleanup-only context не восстанавливает grant
и не позволяет Create. Runtime подключает WindowsSession; actual mutations не
проверены, выполнено только read-only чтение native tables.

Packet guards дополняют ALE ACL: protected IP разрешён только через actual owned
interface index/LUID. Removing journal IP не теряет guard. Teardown сначала
снимает permits, затем удаляет guards после native absence check. Persistent
deny tier отделён от dynamic permits; kernel ID запретов записываются в protected
prepared journal до BFE commit. Recovery восстанавливает только cleanup context,
проверяет exact proof/ID и не создаёт permits. При uncertain abort отдельный
ReleaseForRecovery закрывает handles, сохраняя committed denies. Подключение к
daemon выполнено; фактическая WFP classification/process-loss проверка не выполнена.

## MVP v0: собираемая поставка

Windows entrypoint `cmd/client/main_windows.go` собирает protected state, enrollment, connectivity005,
Windows IPC и lifecycle через `clientapp.NewWindows` и запускает SCM host.
Обязательные ENV и build metadata проверяются до обращения к SCM; отсутствующее
state при обычном запуске не инициализируется. Явная offline-команда `client init-state`
создаёт только пустое protected state с теми же validated ENV и build metadata.
Её выполняют под настроенным ServiceSID в заранее подготовленном каталоге с
проверенным owner/DACL; команда не создаёт каталог, не меняет ACL и не устанавливает
или запускает службу. Existing/corrupt state не перезаписывается, migration/reset
и генерация keys/attempt/credentials не выполняются. Для v0.6 эта команда не является
переходом на новую версию. Установка и запуск установленной службы здесь не выполняются.
Другие платформы пока сохраняют сборочный каркас `cmd/client/main.go`.
Approval/recovery, legacy adoption и системная приёмка003 ещё не завершены.

Private browser approval state хранит исходный device-only request и receipt под
тем же OS protector. Явный browser class допускает name-only pending; original
keys/UUID/token/decision/deadlines не заменяются при retry/reopen. Наблюдение
approved/enrolled не даёт credential. Native direct registration отвергает browser
class. Pure PrepareApprovalState не выполняет I/O; product entrypoint через IPC/UI
ещё не добавлен. NewWindows связывает executor/worker для восстановления уже
сохранённого browser-класса. Обязательный `ENDLESSNET_CLIENT_APPROVAL_BASE` задаёт
trusted HTTPS URL, совпадающий с private state; defaults или URL из ошибки нет.

Standalone ApprovalExecutor сохраняет marker до original Create/Poll/Complete и
атомарно принимает snapshot/context/verified credential. Lost reply/local ACK
восстанавливаются с тем же input/token и fixed recovery. NewApprovalEnrollmentWorker
явно включает browser phases и startup recovery; прежний constructor их не
исполняет. Current purpose trust/signature проверяются перед credential commit;
Cancel не запускает новый remote вызов. Это consumer core с transport fixtures,
не сквозная проверка actual Coordinator/Management или активация службы.

Isolated Windows App/HTTPS/DPAPI/IPC fixture проверяет два lifetimes для lost Create
и lost Complete: original identity/request/token/deadlines сохранены, результат
durable, IPC показывает registered/disconnected и только flags наличия credential/
fingerprint. Installed service/state не используются. Client API0.2.0 определяет
EnrollmentBrowserAction как Identity URL; Coordinator approval action требует
отдельного API-first amendment вместе с выбранным product entrypoint.
Профиль модулей и артефактов: [.ci/profile.json](.ci/profile.json).
Helper build/publish требует чистый committed checkout до и после сборки.
Output размещается вне checkout либо в ignored каталоге `.artifacts`.
Локальные изменения проверяются командой check; они не публикуются как bytes HEAD.
Branch CI проверяет код и собирает outputs. Push обычного v0.MINOR.PATCH выпускает
runtime/source; независимый api/v0.MINOR.PATCH — только API/SDK, где объявлено.
Новая v0-линейка не поддерживает обратную совместимость, включая patch;
сохранение данных прямой миграцией не означает поддержку прежнего бинарника.

Build helper закрепляет version/tag, exact clean commit и дату commit в Windows
бинарнике. IPC digest проверяется по descriptor опубликованного Client SDK0.2.0,
его resolved version/directory и Origin.Hash. Проверки и ограничения описаны в
[AGENTS.md](AGENTS.md); успешная сборка не является продуктовой приёмкой.

Отдельный Windows integration test
`TestWindowsRuntimeAgainstActualOwnerChain` принимает путь
`ENDLESSNET_CLIENT_OWNER_CHAIN_FIXTURE` к Temp JSON с `address` (loopback), `ca`
(base64 PEM) и `bootstrap` (base64 Signing JSON). Внешний изолированный fixture
готовит Account/Network и native session, явно заданные в test source. Проверка
использует настоящие DPAPI/pipe, List/Select через SDK, теряет committed response
и восстанавливает тот же outcome после нового App lifetime. Без explicit input
этот test пропускается; обычный unit gate не доказывает cross-owner acceptance.
Installed Client, его state, системные roots и SCM installation не используются.

Optional поле `browser_simulator` задаёт адрес loopback controller тестового
браузера. В этом режиме initial profile содержит только подготовленное доверие
серверу: ключи/попытка создаются настоящим IPC `Enroll`, native session выдаёт
Identity после PKCE/callback, затем выполняются List/Select и registration recovery.
Controller readiness проверяется отдельно; login POST не повторяется автоматически.
VK wire fixture не означает вход через настоящий аккаунт VK.

`TestWindowsUIAgainstActualOwnerChain` дополнительно запускает opt-in Flutter
controller/panel test через настоящий pipe. Нужны explicit
`ENDLESSNET_CLIENT_UI_OWNER_APP` и `ENDLESSNET_CLIENT_UI_FLUTTER_ROOT`; обычные
Client build/unit checks не требуют UI или Flutter. Два lifetimes сохраняют
Client keys/request/deadlines и UI correlation journal без второго enrollment.

## Установка на Ubuntu

Для Ubuntu26.04 LTS amd64 используется существующий APT-репозиторий.
Из checkout Client запустите `sudo sh scripts/install.sh`.
[Скрипт](scripts/install.sh) подключает signed-by источник и вызывает
`apt-get install endlessnet-client`; запуск службы выполняет сам пакет.
Это APT-установка без ручной распаковки или сборки бинарника.
Проверка команд без изменения системы: `python3 -B scripts/test_install.py`
в Ubuntu26.04. Проверка реальной установки на отдельном хосте ещё требуется.

Скрипт также устанавливает команду `endlessnet` в `/usr/local/bin`.
Проверка обновлений: `sudo endlessnet check-updates`.
Команда обновляет APT-индекс, показывает установленную/доступную версии и
спрашивает подтверждение. Только ответ `y`/`yes`/`да` запускает APT upgrade;
отмена и EOF ничего не устанавливают. Данные пакета не удаляются.
Unit-проверка команды: `python3 -B scripts/test_endlessnet.py`.

## Подключение Windows-установщика

Внутренние entrypoints `installed-init` и `installed-run` требуют SYSTEM и
читают REG_MULTI_SZ `Environment` из собственного SCM service key.
`installed-init` использует существующую строгую инициализацию: существующий
state не заменяется. MSI должен вызывать её только для новой установки.
`installed-run` запускает тот же Windows runtime с этими настройками.

`ENDLESSNET_CLIENT_RUNTIME_INSTANCE_FILE` задаёт путь публичного файла UUID
текущего IPC instance. Parent создаёт MSI: SYSTEM/Admin могут писать, UI может
читать; каталог state остаётся закрытым. UUID публикуется после IPC startup
и удаляется при остановке только если принадлежит текущему процессу.
Это адрес подключения, а не подтверждение identity или прав пользователя.

Для builder доступен `validate-config`: проверяет текущие environment settings
и build identity без открытия/создания state и без запуска службы.

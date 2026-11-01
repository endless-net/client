# client

## MVP v0: собираемая поставка

Windows entrypoint `cmd/client/main_windows.go` собирает protected state, enrollment,
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

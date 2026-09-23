# Состояние client cutover — 2026-09-15

Актуальное дополнение 2026-09-22, block 2: LAN_ALLOW интегрирован в Linux
native exit через существующие host/worker и durable store. Финальные NIC,
маршрут без gateway и абсолютный срок проверяет BPF; точные адреса и output
interface повторно проверяет nft после routing/NAT. Добавлены LAN routes/rule
с terminal prohibit, проверка pins/health/topology/journal, закрытие при потере
подтверждений и точная очистка перед удалением journal. Saved resume, reapply,
Stop/Clear используют общий lifecycle. DHCP/ND отделены от LAN приложений.
Подробности и границы evidence: [runtime gaps](client-runtime-implementation-gaps.md#integrated-lan_allow-increment--2026-09-22).
Старые BLOCK-only/preparation-only записи ниже исторические. Native verifier,
traffic и полный system acceptance ещё не подтверждены; координатор и общая
цель cutover этим не завершаются.

Дополнение от 2026-09-21: добавлены durable checkpoint перед exit release и
отдельная запись владения защитой, переживающая terminal containment; native
adapter/worker всё ещё требуется. Для resources добавлены retirement/возврат
локальных choices при проверенной смене map, repair orphan state и recovery
после смены trust или утраты старого cache. Это не подтверждает реальную
доступность ресурсов. Удалён неиспользуемый HTTP/1 server helper; CI concurrency
разделяет short push и manual qualification и отменяет устаревшие однотипные runs.

Следующий increment от 2026-09-21 добавляет строгий readback nft после установки
и удаления защиты, а также ограниченную повторную блокировку при неоднозначном
Open/Release. Проверяются собственные цепочки, policy/drop, exemptions и TUN
gate; отсутствие таблицы подтверждается успешным чтением. Production adapter,
ongoing invalidation и реальные platform evidence по-прежнему не завершены.

DNS increment от 2026-09-21: добавлены захват независимого Linux DNS source,
прямое разрешение разрешённых control/relay имён и передача immutable snapshot
через engine/transport/recovery. Смена owner, DNS или адресов внешних интерфейсов
отклоняет старый источник. Общая отзываемая DNS-сессия теперь закрывает живые
соединения и отменяет HTTP body; global/per-link DNS route проверяется до/после
connect. Это периодические наблюдения: изменения только маршрутов/firewall ещё
требуют отдельной invalidation, а native socket effects — приёмки.
DNSSEC/DoT режимы пока отклоняются без downgrade.

Restart cleanup increment от 2026-09-21: остановленный engine восстанавливает
containment и очищает подтверждённые default routes и policy rules по durable
interface/table. Main suppression теперь привязан к mark; чужие/неоднозначные
правила не удаляются. Частичный результат повторяется после рестарта без
router.current. Reserved tables отклоняются до изменений ОС. Это не завершает
native adapter/worker и не заменяет платформенную приёмку.

Полная цель не завершена. Релиз v0.6.0 опубликован, но публикация не является
приёмкой всех требований BA/SA. Текущие изменения после релиза находятся в main.

Lifetime/resume increment от 2026-09-21: worker получил периодическую проверку
защиты и отдельный callback восстановления сохранённого selection. Проверяется
и firewall остановленного runtime; при нарушении сначала закрывается packet
filter, затем подтверждается containment. Shutdown отменяет ожидание lock и
дожидается монитора. После resume повторно проверяется сохранённый контекст.
UAPI readback теперь проверяет также локальную WireGuard identity и PSK.
Подключение worker к host/map loop, admission при отключённом клиенте,
восстановление withdrawn live runtime и capabilities/events ещё требуют работы.

Native adapter increment от 2026-09-21: добавлены callbacks Apply/Clear/Contain/
Release с привязкой к durable operation и независимыми наблюдениями, а также
чтение runtime exit status под общим lock с повторной проверкой прав и контекста.
Single-family firewall сохраняет обычную политику невыбранной семьи. После
подтверждённого containment ошибка control authority не блокирует запуск
локального recovery RPC. Запуск worker в агенте, восстановление сохранённого
selection, постоянная invalidation и LAN_ALLOW ещё не завершены.

## Реализовано и остаётся

Восстановление сверяет оставшиеся pins с журналом и удерживает найденные FD.
Неполный набор допустим; чужие объекты не изменяются. Lease отзывается только
через подтверждённую map. Detach, удаление pins и завершение recovery ещё нужны.

Native factory объединяет закрытую подготовку LAN: namespace → nft BLOCK →
attach → durable checkpoint/pins → readback. Сессия удерживает ресурсы и при
Close отзывает lease, сохраняя pins для recovery. LAN_ALLOW ещё не включён.

Добавлена граница boot/netns: удерживаемый FD, проверка до/после callback на
закреплённом OS-потоке, запрет повторного использования после потери наблюдения.
Журнал другого boot/namespace отклоняется до операций восстановления.
Подключение этой границы к native LAN и очистка pins ещё требуются.

Создание pins требует успешного durable checkpoint полного журнала до первого
pin syscall. Ошибки и отмена при сохранении не создают pins; при частичном сбое
журнал остаётся для восстановления. Native boot/netns binding ещё требуется.

Добавлен сохраняемый журнал BPF-владения в exit-защите с атомарным checkpoint
обеих копий и защитой от замены записи. Clear не удаляет защиту, пока журнал
не очищен. Native capture boot/namespace и восстановление/очистка pins ещё нужны.

Публикация LAN deadline требует проверки активных hooks выбранных семейств
до и после записи под общей блокировкой. Ошибка отзывает разрешение. Владение
pins после рестарта, namespace binding и интеграция LAN_ALLOW остаются открытыми.

LAN family increment: links и pins создаются только для выбранных IP-семейств;
ошибка выбранной семьи не понижает dual-stack. IPv6-only получает `_ipv6` pin
независимо от позиции в массиве. Topology evidence привязано к режиму семейства.
Открытие LAN в native adapter пока не включено.

LAN hook increment: добавлен native netlink dump с проверкой kernel sender,
полного завершения и точного BPF program/family/hook/priority. Потеря сообщений,
прерванный dump, повторные совпадения и отмена не дают подтверждения. Это readback
на момент запроса, пока без интеграции в LAN adapter и без нативной приёмки.

LAN pin increment: добавлены exclusive pins карты, программы и обеих links
относительно открытого bpffs-каталога, повторное открытие и сверка object ID.
Частичные pins сохраняются закрытыми; чужие объекты не заменяются/не удаляются.
Startup recovery, durable ownership и live hook readback ещё не завершены.

LAN link increment: добавлены создание закрытых IPv4/IPv6 netfilter links,
проверка object identity и cleanup частично созданной пары. Это ещё не pinning
и не подтверждение активных hooks: metadata сохраняется после detach.
Native adapter пока не использует этот primitive для открытия LAN.

LAN BPF increment: реализованы генерация deadline-программы, загрузка закрытых
неподключённых объектов и атомарная публикация immutable lease через map-in-map.
Ошибка обновления отзывает прежнее разрешение; отмена при занятом publisher
не обещает отзыв и требует отдельного containment. Bytecode и ABI проверяются
интерпретатором и fake syscall. Pinning links, live hook readback, повторное
использование evidence, recovery и интеграция adapter остаются открытыми;
реальная загрузка и действие на пакеты ещё не подтверждены.

LAN clock increment: добавлен фиксированный абсолютный срок по CLOCK_BOOTTIME,
снятый до подготовки плана. Повторная проверка не обновляет срок; задержка,
сон и скачок realtime вперёд могут только закрыть подготовленное разрешение.
Ограничение между повторными preparations и recovery ещё не реализованы.
Публикация BPF lease реализована отдельно, без live attachments.
Native LAN_ALLOW остаётся закрытым.

HOST observation lifetime increment: сроки map, handshake и выбранного exit grant
проверяются после финального readback. Завершение relay во время чтения снимает
подтверждение. Это закрывает окна устаревшего HOST AVAILABLE, но не подтверждает
доступность приложений/SERVICE и не заменяет native приёмку.

Решение LAN_ALLOW от пользователя (2026-09-21): непосредственно подключённые
подсети физических Ethernet/Wi-Fi интерфейсов, включая публичные; без ручных CIDR.
RFC1918/ULA сами по себе ничего не разрешают. Gateway/VPN/bridge/container
исключены, разрешение связано с точным prefix и экземпляром интерфейса;
overlay/resource routes приоритетны. Потеря exit или неподтверждённое состояние
закрывает LAN. Application link-local и broadcast/multicast discovery пока
исключены, DHCP/ND обслуживаются отдельно. Семантика согласована, реализация
и native приёмка ещё не завершены.

LAN preparation increment от 2026-09-21: добавлен Linux collector кандидатов
по `ip` JSON и фиксированным sysfs attributes. Два снимка сопоставляют link,
driver/bus, адреса с prefix lengths и direct main routes; gateway/via/ECMP,
virtual links и неоднозначные attachments исключаются. Сохраняется самый ранний
срок действия адресов. Отдельный planner проверяет signed exit grant и вычитает
overlay/resource/sticky application destinations без расширения CIDR, сохраняя
исходный prefix, interface и source addresses. `/31` и `/32` не теряют адреса
как обычные broadcast-подсети; пустая выбранная семья не считается ALLOW.

Это подготовка кандидатов, пока не подключённая к native Apply. Sysfs не исключает
эмуляцию аппаратного NIC в VM; physical classification требует qualification.
Двойной snapshot не доказывает непрерывность экземпляра интерфейса. Следующий
datapath должен использовать собственную direct-route table с terminal prohibit,
защищённую установку по instance, исключения ресурсов и истекающую в kernel lease,
ограниченную свежим exit-health evidence. LAN_ALLOW остаётся недоступным.

LAN evidence increment от 2026-09-21: подготовка связывается с authenticated
handshake выбранного exit peer и текущим direct path либо поколением relay.
Повторное чтение не продлевает исходный срок; проверка после последнего readback
отклоняет уже истёкшие данные. IPv6 RA expiry ограничивает срок topology, а
router preference входит в сравнение снимков. Это подтверждение peer transport,
не Internet forwarding и не завершённая реализация native LAN_ALLOW.

LAN topology lifetime increment: подписка на Linux link/address/route/rule
запускается до снимков. Событие, потеря уведомлений или остановка подписки
необратимо снимает подтверждение; копии и готовый план сохраняют исходный
lifetime. Финальный readback повторно проверяет его. Это ещё не packet-time
привязка к экземпляру NIC и не отслеживание Wi-Fi association; native Apply
по-прежнему не разрешает LAN_ALLOW.

Relay HOST increment от 2026-09-21: observer связывает подписанную map и peer
с текущим экземпляром relay bridge, его локальным UAPI endpoint и выбранным
внешним relay. Подтверждение требует полного timestamp handshake строго после
готовности bridge и перехода пути; точность наносекунд остаётся приватной и не
меняет wire/JSON контракт. Остановка, замена bridge или отзыв DNS lease снимают
подтверждение. Native route/rule/filter проверки сохраняются. Direct path также
требует handshake после перехода; продолжающаяся старая сессия без нового
handshake пока не даёт положительного результата. Это transport evidence HOST,
а не проверка приложения; native relay traffic ещё требует приёмки.

Worker lifetime increment от 2026-09-21: ожидание worker/effect mutex теперь
отменяемо у profile, connection, preferences, enrollment, session, trust и
network-selection workers. Отмена ожидания сохраняет journal и освобождает уже
захваченные mutex; host worker завершается даже при занятом внешнем effect lock.
Отдельная проверка resources проходит через admission, native apply, реальные
packet-filter запрет/разрешение, replay и maintenance сохранённого exit.

Transition increment от 2026-09-21: Clear после смены имени TUN/route table
использует исходную durable область защиты; после Clear старые артефакты и новый
ordinary runtime проверяются независимо. Select/Resume не переносят selection
на другой интерфейс автоматически. Preferences/resources при сохранённом exit
получили отдельное применение точного RUNNING candidate с native readback,
повторной проверкой store и containment при ошибке. Отмена между Start и commit
вызывает bounded Stop с сохранением журнала. Уведомление offline после Disconnect
также использует transport engine без обычного HTTP fallback.

Resources/probe increment от 2026-09-21: Linux HOST observer проверяет реальные
route lookup, адрес интерфейса, routing rules, live UAPI и свежий direct path.
Смена scope, просроченная map и ограничения ACL/application/sharing исключают
положительный результат; resource denials сохраняют приоритет. Чтение ОС идёт
вне RPC mutex, а изменения host evidence инвалидируют resources. Последующий
relay increment описан выше; другие виды ресурсов требуют отдельных наблюдений. Readyz теперь использует
транспорт engine и не переходит на обычный HTTP при отказе защищённого транспорта.
Локальный standalone status без runtime transport показывает сохранённые факты
без readiness-запроса. Проработка семантики LAN_ALLOW передана отдельной задаче
в проекте архитектуры по указанию пользователя.

Host/restart increment от 2026-09-21: Linux agent запускает native exit-worker
независимо от включённого IPC listener; supervisor отменяет runtime и дожидается
workers до закрытия engine. Capability принадлежит живому native worker.
Connect с сохранённым exit использует защищённый resume. Select требует явного
connected intent и не выполняет Connect; Clear и replay сохраняются отдельно.
Область проверки после Clear теперь сохраняется атомарно с завершением операции
и восстанавливается после рестарта без credentials и без доверия старому success:
каждое наблюдение заново проверяет ОС.

Clear обычного работающего туннеля проверяет сохранённую engine identity и
actual UAPI до принятия защиты; чужой owner/profile с тем же node не затрагивается.
Rollback восстанавливает эту identity вместе с устройством. Непригодная route
table отклоняется до создания Select/Clear journal.

Read-model increment от 2026-09-21: после Clear native observer заново проверяет
отсутствие exit firewall/rules/default routes и actual UAPI обычного runtime.
Приватная запись определяет только область проверки и не заменяет свежие
наблюдения; более поздний host/restart increment сохраняет область на диск. Каталог показывает
только разрешённые policy и поддержанные worker комбинации family/LAN, а control
отражает admission для Clear. Worker отдельно публикует изменения наблюдаемого
состояния; при смене готовности подписчики получают STALE_STATE для rebootstrap.
Capability теперь включается только публичным native host adapter.
Основной map loop теперь пропускает apply при незавершённом ExitChange.

| Область | Что реализовано | Что ещё требуется |
| --- | --- | --- |
| IPC и потребители | Protobuf client.v0, generated API, локальный gRPC через pipe/Unix socket; CLI и recovery helper используют v0 | Полный аудит удаления legacy, совместной работы Go/Dart и всех потребителей; приёмка UI относится к внешнему репозиторию |
| Состояние и операции | Durable операции, идентификаторы запросов, replay, проверки владельца/профиля, конфликты и восстановление; snapshot/events | Проверка каждой мутации и перехода по матрице, включая права, CAS, отмену, рестарт и приватность событий |
| Enrollment, trust, session | Workers и транспорт enrollment/trust/renewal, сохранение прогресса, ротация credentials, отмена старых запросов и защита от поздних ответов | Полный аудит cleanup/concurrency и контекста; подтверждение producer semantics и реального seamless renewal |
| Exit | Linux native host/worker независимо от IPC, durable Select/Clear/ownership, защищённый saved resume, fresh UAPI/routes/firewall/DNS observations, catalog/control/events/capability; Clear/restart по исходному scope после смены интерфейса; guarded preference/resource candidate; интегрированный LAN_ALLOW с BPF/nft/route lease и cleanup | Приёмка LAN_ALLOW, полная совместная работа с profile/network transitions, автоматическая миграция интерфейса, проверка live recovery и OS qualification; Select-before-Connect сейчас отклоняется |
| Resources | SetResourceEnabled с durable worker, policy/overlap, фильтрация TUN и наблюдения запретов; retirement/возврат choices; Linux HOST route/UAPI/direct/relay-path observer, fresh publication и invalidation | Subnet/service/application observations, полный rollback/restart audit и приёмка реальных OS/relay-эффектов |
| Preferences и lifecycle | Set/Reset inbound/DNS/routes и lifecycle-полей, managed policy/locks/source; Windows power/logoff paths с ожиданием suspend teardown в SCM callback (25 с) и resume-policy refresh; Linux logind sleep + UID-bound session removal с source recovery; macOS IOKit sleep/wake с ожиданием executor и native arm64/amd64 core artifacts; отсутствующие OS-event sources дают UNSUPPORTED | Полный аудит эффектов каждой настройки, macOS UID-bound logoff source (AppKit session switch и active console user не доказывают logout), native sleep/logoff/SCM qualification; задержка logind ограничена `InhibitDelayMaxUSec` |
| Networks/profiles | Контекстные проверки, guards переключения, изоляция состояния и защита от устаревших ответов | Полная смена identity/map/routes, recovery и фактическая изоляция при переключении сети/provider |
| Diagnostics и updates | Ограниченные диагностические данные/экспорт, честные unavailable-состояния; Linux/Darwin exact-destination route samples; явные маркеры отсутствующего OS resolver/default-route/resource observation | Полнота routes/resources и аудит privacy/bounds; согласованный проверяемый distribution source и проверка установленной пары UI/core. Сейчас GetUpdateInfo возвращает SOURCE_UNAVAILABLE |
| Release/CI | [v0.6.0](https://github.com/endless-net/client/releases/tag/v0.6.0), публикация APT; push запускает short-тесты root и clientipc | Итоговая приёмка полного cutover отдельным согласованным интеграционным/системным/платформенным этапом |

## Последние изменения exit

- Проверяется прямой default route на ожидаемый интерфейс для каждой включённой
  IP-семьи; неоднозначные, непрямые и link-down маршруты не подтверждают успех.
- Проверяются not-fwmark/table и main/suppress selectors, их приоритеты,
  отсутствие дубликатов и раннего main lookup, способного перехватить маршрут.
- Для отключённой IP-семьи проверяется отсутствие exit-таблицы, ссылок на неё
  и main suppression. Ошибка команды или отмена не считается отсутствием.
- При неподтверждённом результате engine не открывает трафик и не сообщает успех.
  Покрытие использует injected команды/engine: реальные firewall/packet-path
  эффекты этими unit-тестами не доказаны.

## Следующие работы

Актуальная очередь после сверки исходников 2026-09-22:
[конечные пакеты работ и критерии готовности](client-cutover-execution.md).
Она отделяет отсутствующее поведение от уже подключённых workers и недостающей
приёмки. Исторические записи ниже не являются текущим перечнем пробелов.

Изменения от 2026-09-21 прошли `goimports -w .`, `go vet ./...`,
`golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m` (0 issues)
и `go test -short ./...`: после LAN clock/BTF increment internal/client
прошёл за 139.047 s, CLI за 10.687 s; остальные пакеты прошли или использовали cache.
Во время предыдущего LAN evidence increment один прогон
упал в internal/client, но assertion потерялся в усечённом выводе; повтор без
изменений с полным логом прошёл. Причина этого непостоянного сбоя не установлена.
Review исправило проверку deadline после финального UAPI/relay readback.
Локальные native/E2E/installer проверки не запускались.

1. Провести native acceptance LAN_ALLOW; завершить сочетания exit с profile/network
   transitions, аудит withdrawn live recovery и автоматическую миграцию
   интерфейса с сохранённым exit. Explicit Clear уже очищает исходный scope. Точный
   combined IPv6 ND readback и socket effects остаются предметом native qualification.
2. Довести resources, preferences/lifecycle и переключение сетей до реальных
   эффектов с положительными и отрицательными unit assertions.
3. Закрыть session/enrollment/trust/diagnostics audit и определить distribution
   source с ответственным владельцем внешнего контракта.
4. Сверить все IT-01–33, BR/AC-01–20, RULE-01–16, US-01–14 и локальные
   обязательства: реализация → конкретное assertion → внешняя зависимость.
5. После полноты реализации/unit-аудита согласовать и выполнить интеграционную,
   системную и платформенную приёмку. Не считать зелёный short CI её заменой.

Подробные реестры: [runtime gaps](client-runtime-implementation-gaps.md),
[headless matrix](client-headless-requirement-map.md),
[local matrix](client-local-requirement-map.md),
[cutover gates](client-ipc-cutover.md). Их исторические записи и ссылки на тесты
являются входом для аудита, а не утверждением о закрытии требований.

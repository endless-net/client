# Состояние client cutover — 2026-09-15

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

Read-model increment от 2026-09-21: после Clear native observer заново проверяет
отсутствие exit firewall/rules/default routes и actual UAPI обычного runtime.
Приватная запись определяет только область проверки и не заменяет свежие
наблюдения; её восстановление после рестарта ещё требуется. Каталог показывает
только разрешённые policy и поддержанные worker комбинации family/LAN, а control
отражает admission для Clear. Worker отдельно публикует изменения наблюдаемого
состояния; при смене готовности подписчики получают STALE_STATE для rebootstrap.
Capability пока не включена: production host ещё не запускает exit-worker.
Основной map loop теперь пропускает apply при незавершённом ExitChange.

| Область | Что реализовано | Что ещё требуется |
| --- | --- | --- |
| IPC и потребители | Protobuf client.v0, generated API, локальный gRPC через pipe/Unix socket; CLI и recovery helper используют v0 | Полный аудит удаления legacy, совместной работы Go/Dart и всех потребителей; приёмка UI относится к внешнему репозиторию |
| Состояние и операции | Durable операции, идентификаторы запросов, replay, проверки владельца/профиля, конфликты и восстановление; snapshot/events | Проверка каждой мутации и перехода по матрице, включая права, CAS, отмену, рестарт и приватность событий |
| Enrollment, trust, session | Workers и транспорт enrollment/trust/renewal, сохранение прогресса, ротация credentials, отмена старых запросов и защита от поздних ответов | Полный аудит cleanup/concurrency и контекста; подтверждение producer semantics и реального seamless renewal |
| Exit | Get/Select/Clear handlers, durable journal и ownership, checkpoint release; Linux executor, cleanup, family-scoped firewall, UAPI/identity/DNS observations; worker maintenance, saved-resume, catalog/control и события; свежий status после Clear в текущем процессе | Host integration, disconnected admission и live recovery, восстановление cleared observation scope после рестарта, capability, LAN_ALLOW и OS qualification |
| Resources | SetResourceEnabled с durable worker, policy/overlap, фильтрация TUN с проверкой адреса/протокола/порта, наблюдения запретов; retirement/возврат choices при аутентифицированной смене map | Положительное подтверждение доступности route/path/application, полный rollback/restart audit и реальные OS-эффекты |
| Preferences и lifecycle | Set/Reset inbound/DNS/routes и lifecycle-полей, managed policy/locks/source; Windows power/logoff paths и resume-policy refresh | Полный аудит эффектов каждой настройки, доставка событий на других ОС, восстановление источников событий и native qualification |
| Networks/profiles | Контекстные проверки, guards переключения, изоляция состояния и защита от устаревших ответов | Полная смена identity/map/routes, recovery и фактическая изоляция при переключении сети/provider |
| Diagnostics и updates | Ограниченные диагностические данные/экспорт, честные unavailable-состояния; route samples | Полнота routes/resources и аудит privacy/bounds; согласованный проверяемый distribution source и проверка установленной пары UI/core. Сейчас GetUpdateInfo возвращает SOURCE_UNAVAILABLE |
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

Изменения от 2026-09-21 прошли `goimports -w .`, `go vet ./...`,
`golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m` (0 issues)
и `go test -short ./...`: internal/client 104.634 s в итоговом запуске
с post-Clear readback, catalog/control и observation events; остальные пакеты прошли.
Локальные native/E2E/installer проверки не запускались.

1. Связать native exit executor, saved resume и maintenance с агентом и map loop;
   согласовать эффекты Select с disconnected intent, восстановить withdrawn live
   runtime и восстановление области наблюдения после Clear через рестарт.
   Подключить capability и lifetime независимо от IPC listener, завершить LAN_ALLOW и recovery при
   изменении настроенного интерфейса с сохранённым старым protection scope. Точный
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

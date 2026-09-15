# Состояние client cutover — 2026-09-15

Полная цель не завершена. Релиз v0.6.0 опубликован, но публикация не является
приёмкой всех требований BA/SA. Текущие изменения после релиза находятся в main.

## Реализовано и остаётся

| Область | Что реализовано | Что ещё требуется |
| --- | --- | --- |
| IPC и потребители | Protobuf client.v0, generated API, локальный gRPC через pipe/Unix socket; CLI и recovery helper используют v0 | Полный аудит удаления legacy, совместной работы Go/Dart и всех потребителей; приёмка UI относится к внешнему репозиторию |
| Состояние и операции | Durable операции, идентификаторы запросов, replay, проверки владельца/профиля, конфликты и восстановление; snapshot/events | Проверка каждой мутации и перехода по матрице, включая права, CAS, отмену, рестарт и приватность событий |
| Enrollment, trust, session | Workers и транспорт enrollment/trust/renewal, сохранение прогресса, ротация credentials, отмена старых запросов и защита от поздних ответов | Полный аудит cleanup/concurrency и контекста; подтверждение producer semantics и реального seamless renewal |
| Exit | Get/Select/Clear handlers, readiness gate, durable journal, worker с injected executor; Linux containment и engine hooks, проверка маршрутов/правил перед открытием | Production native adapter и запуск worker в агенте; полноценный Clear с crash recovery, DNS до exit, владение интерфейсами/правилами после рестарта, firewall readback, OS/LAN qualification |
| Resources | SetResourceEnabled с durable worker, policy/overlap, фильтрация TUN с проверкой адреса/протокола/порта, наблюдения запретов | Положительное подтверждение доступности route/path/application, stale choices, полный rollback/restart audit и реальные OS-эффекты |
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

Последняя правка прошла `goimports -w .`, `go vet ./...`,
`golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m` (0 issues)
и `go test -short ./...` (включая internal/client: 110.915 s).
Локальные native/E2E/installer проверки для неё не запускались.

1. Завершить native exit Apply/Clear/Contain и recovery; связать его с агентом
   только после реализации необходимых эффектов и наблюдений.
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

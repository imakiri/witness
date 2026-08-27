# Migration log — `service_name` + Grafana drill-down + Observer hardening

Изменения, накопленные на ветке `feat/otlp` поверх предыдущей итерации Postgres-наблюдателя. Состоит из четырёх логических фаз; результат — `service_name` как first-class-данные, надёжный батчинг-наблюдатель, плагин с native-tracing и Grafana-дашборд с трёхуровневым drill-down (trace → service → span → events).

## Краткая сводка

| Что было | Что стало |
|---|---|
| `service_name` неявно сидел в `event_message` события `span:instance:online`; дашборд угадывал сервис по хардкод-`CASE WHEN span_name IN (…)` | Колонка `witness.events.service_name`, поле `Context.serviceName` и `Event.ServiceName`, Observer пишет напрямую. Все запросы — `WHERE service_name = …` |
| Postgres Observer: race `Observe`/`Close`, `time.Tick` leak, потеря событий на shutdown, `context.Background()` в `SendBatch`, нет валидации `Config`, нет идемпотентного `Close` | Single-select `Observe` (drop-newest + `atomic.Uint64` counter), `sync.Once`-`Close` с дренажом до `ShutdownTimeout`, `time.NewTicker`, `BatchTimeout` на каждый `SendBatch`, валидация `Config`, `Dropped()` метод |
| Дашборд: 2 панели (table + logs), хардкод-маппинг сервисов в SQL, нет графического представления вызовов, drill-down ограничен `selected_trace` → logs всего трейса | 3-уровневый drill-down: L1 recent traces → L2 (NodeGraph + bar chart + spans table) → L3 logs (фильтр по trace × service × span). Все клики — data links через template variables |
| Плагин: реальный pgxpool-backend с 4 query types (`search`/`trace`/`logs`/`table`) и frontend, но service берётся через JOIN по span-цепочке, нет `service-map` query, нет `/services`/`/operations` resource endpoints | `trace.go` упростил — service-name из `span_pairs` напрямую; добавил query type `traces` (для L1) и `service-map` (NodeGraph frame); добавил resource endpoints `services` и `operations[?service=]`; на frontend — `Service`-поля в Search/Logs editors, `ServiceMapEditor`, `TracesEditor` |
| Миграции: `migration.up.sql` + `migration_v2.up.sql` + `migration_v3.up.sql` нужно было применять подряд; views ссылались на `trace_id`/`parent_*` без явной зависимости | Консолидированная `schema.up.sql` (v1+v2+v3+v4) для свежих деплоев; инкрементальные `migration_v*.sql` для апгрейда существующих инсталляций; views получили `service_name`/`parent_service_name`/`child_service_name`, добавился `trace_services` view |

## Что было до

### Postgres Observer (`observers/postgres/postgres.go`)

7 критических багов lifecycle и батчинга, заглубленных в ~150 строк:

1. **Race condition** `Observe`/`Close` — `select { case <-done: return; default: ch <- event }` оставлял окно между `<-done` (не выбран) и `ch <- event`. После `Close()` это `panic: send on closed channel`.
2. **Goroutine leak** — `time.Tick(d)` per worker, никогда не останавливался.
3. **Event loss on shutdown** — `close(done)` → workers выходили моментально, события в канале терялись.
4. **`SendBatch(context.Background(), …)`** — без таймаута; залипший Postgres = висящий `Close()`.
5. **Не валидируется `Config`** — `CollectionDuration == 0` → `time.Tick(0)` panic; `CollectionMaxSize == 0` → unbuffered channel deadlock.
6. **Нет idempotency `Close()`** — повторный вызов = `close of closed channel` panic.
7. **Расхождение кода и миграций** — Observer вставлял в `trace_id`/`parent_trace_id`/`parent_span_id`, добавленные в `migration_v2`/`v3`. Свежий деплой с одной `migration.up.sql` молча терял все события из-за provider error в каждом батче.

### Grafana monitor

- `witness-overview.json` — 2 панели: spans table (groupToNestedTable) + logs panel keyed на `${selected_trace}`.
- Маппинг сервиса — хардкод `CASE WHEN span_name IN ('service-b', 'service-b-boot', 'process-request') THEN 'service-b' WHEN span_name IN ('service-a', 'handle-work', 'POST service-b /sub') THEN 'service-a' ELSE 'unknown' END`. Добавление нового сервиса = редактирование SQL.
- `witness.cross_service_edges` view существовал, но дашборд не визуализировал граф; графического представления `кто кого зовёт` не было.

### Плагин (`monitors/grafana/plugin/`)

Зрелый, но half-complete:
- Backend на `grafana-plugin-sdk-go`, `pgxpool`-pool, 4 query types работают (`search`/`trace`/`logs`/`table`).
- `trace.go` восстанавливал service-name через JOIN: `events ↔ spans (peer) ↔ events (peer) WHERE event_type = 21`. Медленно на больших трейсах.
- `tracesToLogsOptions` для прыжка из Traces panel в логи — не настроено.
- Resource endpoints — только `event-types`.

---

## Что сделано

### Phase A — `service_name` как first-class данные

**Корневой пакет (`.`):**

- `context.go` — `Context.serviceName string` + публичный геттер `Context.ServiceName()`. `Context.Observe` и `Context.Join` пробрасывают serviceName.
- `observer.go` — `Event.ServiceName string` с docstring.
- `witness.go` — `Instance` и `InstanceContinue` ставят `c.serviceName = instanceName` и в Event'е `span:instance:online`/`offline`; `Trace` и `withChildSpan` пробрасывают unchanged.
- **API не сломан** — `instanceName` уже передавался в `Instance`/`InstanceContinue`, мы просто копируем его в Context. Существующий код (включая `examples/distributed`) работает без изменений.

**Postgres Observer (`observers/postgres/postgres.go`):**

- `queueEvent` вставляет `service_name` через `nullString()` хелпер.

**Schema (`observers/postgres/`):**

- `migration_v4.up.sql` (новый) — `ALTER witness.events ADD COLUMN service_name varchar(127) NULL` + `events_service_lookup` индекс. Backfill не делаем (договорились); старые строки = NULL.
- `migration_v4.down.sql` (новый) — обратный rollback.
- `schema.up.sql` (патч) — `service_name` встроен в `CREATE TABLE`, `events_service_lookup` индекс присутствует с самого начала.

**Views (`monitors/grafana/views.up.sql`):**

- `span_starts` и `span_pairs` — теперь экспортируют `service_name`.
- `cross_service_edges` — получил `child_service_name` и `parent_service_name` (через JOIN на `parent_start`).
- `trace_services` (новый view) — `(trace_id, service_name, first_event_at, last_event_at, event_count, error_count)` агрегат для L1 dashboard и L2 NodeGraph.
- `views.down.sql` дополнен `DROP VIEW IF EXISTS witness.trace_services`.

**Тесты (`observers/postgres/postgres_test.go`):**

- `TestObserverPropagatesServiceName` — integration: `witness.Instance("test-service", ...)` → `Span` → `Info` → закрываем; проверяем, что в БД ровно 5 строк, все под `service_name = 'test-service'`.
- Унаследованные unit-тесты (`TestObserverCloseRace`, `TestObserverDropNewestUnderBackpressure`, `TestObserverCloseRespectsShutdownTimeout`, `TestObserverDoubleClose`, `TestObserverDrainsOnClose`, `TestNewObserverRejectsBadConfig`, `TestObserverObserveAfterCloseDoesNotPanic`) — проходят с `-race`.

### Phase B — Плагин под drill-down

**Backend (`monitors/grafana/plugin/pkg/`):**

- `queries/trace.go` — выкинул CTE `span_service`; service-name теперь из `sp.service_name` (через `witness.span_pairs`, который получил колонку в Phase A).
- `queries/search.go` — поле `Service` в `Search` struct → `WHERE e.service_name = $`; в output frame появились поля `service` и `traceID`.
- `queries/logs.go` — поля `Service` и `TraceID` в `LogsReq` → `WHERE e.service_name = $` и `WHERE e.trace_id = $::uuid`; output frame с `service` и `traceID`.
- `queries/service_map.go` (новый) — `ServiceMap{TraceID}` query type. Возвращает два data frame (`nodes`, `edges`) в формате Grafana NodeGraph: узлы из `trace_services`, рёбра из `cross_service_edges`.
- `queries/traces.go` (новый) — `TracesReq{Service, Search}` query type для L1; возвращает таблицу `(trace_id, started_at, duration_ms, event_count, error_count, services)`.
- `plugin/query.go` — роутер расширен `case "service-map"` и `case "traces"`.
- `plugin/resource.go` — два новых endpoint: `/services` (DISTINCT service_name) и `/operations[?service=...]` (DISTINCT span_name из `span_starts`, опционально scoped к сервису).

**Frontend (`monitors/grafana/plugin/src/`):**

- `types.ts` — `QueryType` extended с `'traces' | 'service-map'`; новые `TracesParams`, `ServiceMapParams`, `NameOption` interfaces; `service`/`traceID` в `LogsParams`, `service` в `SearchParams`.
- `datasource.ts` — template-interpolation для новых полей; `getServices()` и `getOperations(service?)` async-методы.
- `components/QueryEditor.tsx` — Service-поля в Search и Logs editors, Trace ID в Logs editor, новые `ServiceMapEditor` и `TracesEditor`.
- `npm run typecheck` чист.

### Phase C — 3-уровневый drill-down дашборд

**Полностью переписан `monitors/grafana/dashboards/witness-overview.json`.** Template variables: `service_filter` (textbox), `trace_search`, `selected_trace`, `selected_service`, `selected_span` (все textbox).

**L1 — Recent traces (Table)**  
SQL агрегирует `witness.events` по `trace_id` в time-window, экспортирует `(trace_id, started_at, duration_ms, event_count, error_count, services)`. Колонки:
- `trace_id` — color-text link → drill-down: `var-selected_trace=${trace_id}&var-selected_service=&var-selected_span=`
- `duration` — gauge gradient
- `errors` — color-background с threshold (зелёный 0 → красный ≥1)
- `services` — comma-separated имена сервисов (`array_agg DISTINCT service_name`)

**L2 — Anatomy of selected trace**:

1. **Service call graph (NodeGraph)**  
   Два target'а: `refId=nodes` из `trace_services`, `refId=edges` из `cross_service_edges` (filtered к `${selected_trace}`). Узлы помечены `mainStat=event count`, `secondaryStat=ms span`. Edge label = `count(*) calls`.  
   Empty-state: `UNION ALL SELECT '— select trace —' WHERE selected_trace = ''` — placeholder-узел, без него Grafana 11.3 кидает "id field is required for nodes data frame".

2. **Per-service events bar chart**  
   Horizontal stacked bars из `trace_services`, color по событиям/ошибкам. Каждый bar несёт data link → drill: `var-selected_service=${service_name}&var-selected_span=`.

3. **Spans of trace (Table)**  
   SQL берёт все спаны trace'а через `witness.spans → span_pairs`, JOIN `span_children` для `parent_span_id`. Колонки:
   - `span` — link → drill: `var-selected_span=${span_id}&var-selected_service=`
   - `service` — link → drill: `var-selected_service=${service_name}&var-selected_span=`
   - `duration_ms` — gauge gradient
   - `parent_span_id`, `span_id`, `caller` — для контекста

**L3 — Logs (trace → service → span drill-down) (Logs panel)**  
Title динамический: `Logs — trace $selected_trace · service ${selected_service:text} · span ${selected_span:text}`.  
SQL приоритеты:
- Если `selected_span` set → `e.event_id IN (SELECT event_id FROM witness.spans WHERE span_id = $selected_span)`. Сервис-фильтр игнорируется.
- Иначе если `selected_service` set → `e.service_name = $selected_service`.
- Иначе → все события трейса.

UUID-cast пустой строки → CASE WHEN-обёртка на оба selected_span/trace, чтобы избежать `pq: invalid input syntax for type uuid: ""`.

### Phase D — Сценарий и визуальная проверка

**Examples (`examples/distributed/`):**

- `service_c/main.go` (новый) — pure-compute сервис на порту :8082, POST /compute. Делает `witness.Span("compute")`, спит 3-10ms, эмитит `log:info` с record'ом `result`.
- `service_a/main.go` — модифицирован: рандомный fan-out (`rand.Intn(3)`): 1/3 only B, 1/3 only C, 1/3 both. Каждый peer-call в отдельной helper-функции `callPeer`.
- `service_b/main.go` — модифицирован: 50% chance после `process-request` сделать `chain-to-c` span и вызвать service_c. Это дает паттерн `a→b→c chain`.

**Grafana docker** — поднят в `witness-grafana-demo` контейнере на порту 13000 (anonymous Admin access, `grafana/grafana:11.3.0`). Provisioning yaml-файлы в `/tmp/witness-grafana/provisioning/` указывают на `witness-test-pg` через `host.docker.internal:5434`. Дашборд — `/tmp/witness-grafana/dashboards/witness-overview.json` (копия из репо).

**Проверено визуально**:
- L1 показывает 30 свежих трейсов в окне `Last 15m` со всеми колонками
- Клик на `trace_id` → L2 NodeGraph рисует `service-a → service-b → service-c` (или `a→b, a→c` для fork-паттерна) со стрелками и mainstat/secondarystat
- Клик на bar в bar chart → L3 фильтрует логи к выбранному сервису (заголовок: `... · service service-b`)
- Клик на `span` в spans table → L3 фильтрует к выбранному span'у (заголовок: `... · span 019ecf67-…`), показывает только события span chain (start / `doing work` / `work done` / finish для `process-request`)

---

## Файлы

### Изменённые

```
context.go                                              # serviceName в Context + геттер
observer.go                                             # ServiceName в Event
witness.go                                              # Instance/InstanceContinue/Trace/withChildSpan
observers/postgres/postgres.go                          # queueEvent INSERT service_name + nullString
observers/postgres/postgres_test.go                     # TestObserverPropagatesServiceName
observers/postgres/schema.up.sql                        # service_name + events_service_lookup
observers/postgres/monitors/grafana/views.up.sql        # service_name в span_starts/span_pairs/cross_service_edges + trace_services view
observers/postgres/monitors/grafana/views.down.sql      # DROP trace_services
observers/postgres/monitors/grafana/dashboards/witness-overview.json    # 3-level drill-down целиком переписан
observers/postgres/monitors/grafana/plugin/pkg/plugin/query.go          # service-map + traces query types
observers/postgres/monitors/grafana/plugin/pkg/plugin/resource.go       # services + operations endpoints
observers/postgres/monitors/grafana/plugin/pkg/queries/trace.go         # service-name напрямую из span_pairs
observers/postgres/monitors/grafana/plugin/pkg/queries/search.go        # Service filter
observers/postgres/monitors/grafana/plugin/pkg/queries/logs.go          # Service + TraceID filters
observers/postgres/monitors/grafana/plugin/src/types.ts                 # new query types + params
observers/postgres/monitors/grafana/plugin/src/datasource.ts            # getServices/getOperations + template interp
observers/postgres/monitors/grafana/plugin/src/components/QueryEditor.tsx   # ServiceMap/Traces editors + Service fields
examples/distributed/service_a/main.go                  # random fan-out B/C/both
examples/distributed/service_b/main.go                  # 50% chain to C
```

### Новые

```
observers/postgres/migration_v4.up.sql                  # ALTER ADD service_name + index
observers/postgres/migration_v4.down.sql                # DROP COLUMN + index
observers/postgres/monitors/grafana/plugin/pkg/queries/service_map.go   # NodeGraph frame
observers/postgres/monitors/grafana/plugin/pkg/queries/traces.go        # L1 traces aggregator
examples/distributed/service_c/main.go                  # compute сервис
observers/postgres/Migration.md                         # этот файл
```

### НЕ трогали

- `migration.up.sql`, `migration_v2.up.sql`, `migration_v3.up.sql` — нужны для инкрементального апгрейда уже задеплоенных инсталляций.
- `go.mod`/`go.sum` — нет новых dependencies.
- Сторонние observers (`stdlog`, `otlp`, `prometheus`, `tee`) — нативный `ServiceName` теперь приходит в `Event`, но они его игнорируют. Если нужно — добавить в отдельной задаче.

---

## Применение

### Свежий деплой

```sh
psql "$WITNESS_DB" \
  -f observers/postgres/schema.up.sql \
  -f observers/postgres/monitors/grafana/views.up.sql
```

### Апгрейд существующей инсталляции (была на schema v3)

```sh
psql "$WITNESS_DB" \
  -f observers/postgres/migration_v4.up.sql \
  -f observers/postgres/monitors/grafana/views.up.sql
```

Старые строки в `witness.events` получают `service_name = NULL`. Дашборд игнорирует их в выборках с фильтром по сервису; в L1 они отображаются как `<unknown>` в L3 (фолбэк через `COALESCE`).

### Откат

```sh
# Down всего, что добавлено
psql "$WITNESS_DB" -f observers/postgres/monitors/grafana/views.down.sql
psql "$WITNESS_DB" -f observers/postgres/migration_v4.down.sql
```

---

## Верификация

```sh
# Unit + integration тесты Observer
WITNESS_TEST_DSN='postgres://witness:witness@localhost:5434/witness?sslmode=disable' \
  go test -race ./observers/postgres/...

# Build плагина (отдельный модуль, вне go.work)
cd observers/postgres/monitors/grafana/plugin && GOWORK=off go build ./...
npm install && npm run typecheck

# End-to-end: examples/distributed
cd examples/distributed
go build -o bin/service_a ./service_a
go build -o bin/service_b ./service_b
go build -o bin/service_c ./service_c

WITNESS_DB='postgres://witness:witness@localhost:5434/witness?sslmode=disable' \
  LISTEN=':28080' SERVICE_B_URL='http://localhost:28081/sub' SERVICE_C_URL='http://localhost:28082/compute' \
  ./bin/service_a &
WITNESS_DB='postgres://witness:witness@localhost:5434/witness?sslmode=disable' \
  LISTEN=':28081' SERVICE_C_URL='http://localhost:28082/compute' \
  ./bin/service_b &
WITNESS_DB='postgres://witness:witness@localhost:5434/witness?sslmode=disable' \
  LISTEN=':28082' \
  ./bin/service_c &

for i in {1..30}; do curl -sf -X POST http://localhost:28080/work; done

# Сверка
psql "$WITNESS_DB" -c "SELECT service_name, count(*) FROM witness.events GROUP BY service_name"
# Ожидаем: service-a, service-b, service-b-boot, service-c, service-c-boot — без NULL

# Grafana
docker run -d --name witness-grafana-demo -p 13000:3000 \
  -e GF_AUTH_ANONYMOUS_ENABLED=true -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
  --add-host=host.docker.internal:host-gateway \
  -v "$(pwd)/observers/postgres/monitors/grafana/provisioning:/etc/grafana/provisioning" \
  -v "$(pwd)/observers/postgres/monitors/grafana/dashboards:/var/lib/grafana/dashboards" \
  grafana/grafana:11.3.0

# Для witness-test-pg на host'е поправь provisioning/datasources/witness.yaml:
#   url: host.docker.internal:5434
```

Открыть `http://localhost:13000/d/witness-overview` — дашборд показывает L1 traces; клик на trace_id раскрывает L2 (NodeGraph + bar + spans table); клик на service или span сужает L3 logs.

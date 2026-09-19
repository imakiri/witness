#!/usr/bin/env bash
# Bring up a witness demo: Postgres, the Grafana monitor, and the three
# services from examples/distributed writing real events into it.
#
#   scripts/demo.sh up        start everything and drive some traffic
#   scripts/demo.sh traffic   drive more traffic through service-a
#   scripts/demo.sh down      stop and remove everything
#
# Postgres is published on PG_PORT (default 55432, so it does not collide
# with a local server on 5432) and Grafana on GRAFANA_PORT (default 3000).
# The services are built and run on the host, which is what makes their
# events look like three processes rather than one.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT=$(pwd)
GRAFANA_DIR=$ROOT/observers/postgres/monitors/grafana

PG_PORT=${PG_PORT:-55432}
GRAFANA_PORT=${GRAFANA_PORT:-3000}
PG_CONTAINER=${PG_CONTAINER:-witness-demo-pg}
GRAFANA_CONTAINER=${GRAFANA_CONTAINER:-witness-demo-grafana}
DSN="postgres://witness:witness@localhost:${PG_PORT}/witness?sslmode=disable"
PIDS=$ROOT/.demo-pids
BIN=$ROOT/.demo-bin

down() {
  if [ -f "$PIDS" ]; then
    while read -r pid; do kill "$pid" 2>/dev/null || true; done < "$PIDS"
    rm -f "$PIDS"
  fi
  # Belt and braces: a service whose pid file was lost still holds its port.
  # The second pattern catches copies started with `go run`, which leaves the
  # binary in the build cache under its package name.
  pkill -f "$BIN/service_" 2>/dev/null || true
  pkill -f "go-build.*/service_[abc]$" 2>/dev/null || true
  docker rm -f "$PG_CONTAINER" "$GRAFANA_CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$ROOT/.demo-dashboards"
  echo "demo down"
}

traffic() {
  local n=${1:-40}
  echo "driving $n requests through service-a"
  for _ in $(seq "$n"); do
    curl -fsS -X POST --data 'work' http://localhost:8080/work >/dev/null || true
    sleep 0.1
  done
  echo "done; events land within CollectionDuration"
}

case "${1:-up}" in
  down) down; exit 0 ;;
  traffic) traffic "${2:-40}"; exit 0 ;;
  up) ;;
  *) echo "usage: $0 [up|traffic [n]|down]" >&2; exit 2 ;;
esac

down

echo "starting postgres on :$PG_PORT"
docker run -d --rm --name "$PG_CONTAINER" \
  -e POSTGRES_PASSWORD=witness -e POSTGRES_USER=witness -e POSTGRES_DB=witness \
  -p "${PG_PORT}:5432" postgres:16-alpine >/dev/null
for _ in $(seq 30); do docker exec "$PG_CONTAINER" pg_isready -q -U witness && break; sleep 1; done

echo "applying schema and views"
docker exec -i "$PG_CONTAINER" psql -q -U witness -d witness < "$ROOT/observers/postgres/000_schema.up.sql"
docker exec -i "$PG_CONTAINER" psql -q -U witness -d witness < "$GRAFANA_DIR/views.up.sql"

# Which dashboards the demo shows. The overview dashboard is left out on
# purpose: the waterfall is what this stack is for, and two provisioned
# dashboards make it easy to end up looking at the wrong one. Add it back by
# listing it here.
DASHBOARDS=${DASHBOARDS:-witness-trace.json}

echo "staging dashboards: $DASHBOARDS"
rm -rf "$ROOT/.demo-dashboards"
mkdir -p "$ROOT/.demo-dashboards"
for d in $DASHBOARDS; do cp "$GRAFANA_DIR/dashboards/$d" "$ROOT/.demo-dashboards/"; done

echo "building the grafana plugin"
# Backend only: the query editor UI is not built, because provisioned
# dashboards carry their queries and building it would pull the whole
# @grafana/create-plugin toolchain in. dist/module.js is hand-written.
(cd "$GRAFANA_DIR/plugin" && GOWORK=off go build -o dist/gpx_witness_linux_amd64 ./pkg)
cp "$GRAFANA_DIR/plugin/plugin.json" "$GRAFANA_DIR/plugin/dist/plugin.json"

echo "starting grafana on :$GRAFANA_PORT"
docker run -d --rm --name "$GRAFANA_CONTAINER" \
  -p "${GRAFANA_PORT}:3000" \
  -e GF_AUTH_ANONYMOUS_ENABLED=true \
  -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
  -e WITNESS_PG_URL="host.docker.internal:${PG_PORT}" \
  -e GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH=/var/lib/grafana/dashboards/witness-trace.json \
  -e GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS=imakiri-witness-datasource \
  -v "$GRAFANA_DIR/plugin/dist:/var/lib/grafana/plugins/imakiri-witness-datasource" \
  --add-host=host.docker.internal:host-gateway \
  -v "$GRAFANA_DIR/provisioning:/etc/grafana/provisioning" \
  -v "$ROOT/.demo-dashboards:/var/lib/grafana/dashboards" \
  grafana/grafana:11.3.0 >/dev/null

echo "building and starting service-a, service-b, service-c"
mkdir -p "$BIN"
: > "$PIDS"
for svc in service_c service_b service_a; do
  # Built rather than run with `go run`: go run's pid is the toolchain's, not
  # the service's, so killing it would leave the service holding its port.
  (cd "$ROOT/examples/distributed" && go build -o "$BIN/$svc" "./$svc")
  WITNESS_DB="$DSN" "$BIN/$svc" >"$ROOT/.demo-$svc.log" 2>&1 &
  echo $! >> "$PIDS"
done

echo "waiting for service-a"
for _ in $(seq 60); do
  curl -fsS -X POST --data warmup http://localhost:8080/work >/dev/null 2>&1 && break
  sleep 1
done
traffic "${2:-40}"

cat <<MSG

Grafana:  http://localhost:${GRAFANA_PORT}  (Dashboards -> Witness)
Postgres: $DSN
Logs:     .demo-service_*.log
Stop:     scripts/demo.sh down
MSG

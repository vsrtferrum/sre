#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
URL="${URL:-http://localhost:30080/}"
DURATION="${DURATION:-60}"
RPS="${RPS:-10}"
KCTX="${KCTX:-kind-hw1}"
K="kubectl --context $KCTX"

echo "== scenario1: load ${RPS} rps for ${DURATION}s, delete pod at t=5s =="

./load/loadgen --url "$URL" --rps "$RPS" --duration "${DURATION}s" \
    --out evidence/scenario1_load.csv &
LOAD_PID=$!

$K get pods -l app=hw1-app -w > evidence/scenario1_pods_watch.log 2>&1 &
WATCH_PID=$!

sleep 5
POD=$($K get pods -l app=hw1-app -o jsonpath='{.items[0].metadata.name}')
echo "== t=5s: deleting $POD =="
$K delete pod "$POD" --wait=false
echo "== snapshot right after delete =="
$K get pods -l app=hw1-app

wait "$LOAD_PID"
kill "$WATCH_PID" 2>/dev/null || true

echo "== final pods =="
$K get pods -l app=hw1-app
echo "== load summary =="
cat evidence/scenario1_load.csv.summary
echo "== requests per pod =="
awk -F, 'NR>1 {print $4}' evidence/scenario1_load.csv | sort | uniq -c
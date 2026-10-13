#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
URL="${URL:-http://localhost:30080/}"
DURATION="${DURATION:-60}"
RPS="${RPS:-10}"
KCTX="${KCTX:-kind-hw1}"
K="kubectl --context $KCTX"

echo "== scenario3: load ${RPS} rps for ${DURATION}s, rollout v1->v2 at t=8s =="

./load/loadgen --url "$URL" --rps "$RPS" --duration "${DURATION}s" \
    --out evidence/scenario3_load.csv &
LOAD_PID=$!

sleep 8
echo "== t=8s: starting rollout v1 -> v2 =="
$K set image deployment/hw1-app hw1-app=hw1-app:v2
echo "== rollout status (background) =="
$K rollout status deployment/hw1-app --timeout=90s &
ROLLOUT_PID=$!

for i in $(seq 1 12); do
    sleep 3
    echo "--- t=$((8 + i * 3))s ---"
    $K get pods -l app=hw1-app -o wide --no-headers
done

wait "$ROLLOUT_PID"
wait "$LOAD_PID"

echo "== final deployment =="
$K get deployment hw1-app -o wide
echo "== rollout history =="
$K rollout history deployment/hw1-app
echo "== load summary =="
cat evidence/scenario3_load.csv.summary
echo "== versions seen in responses =="
awk -F, 'NR>1 {print $3}' evidence/scenario3_load.csv | sort | uniq -c
echo "== version switch timeline (first v2 per second) =="
awk -F, 'NR>1 && $3=="v2" {print $1}' evidence/scenario3_load.csv | sort | uniq -c | head -5
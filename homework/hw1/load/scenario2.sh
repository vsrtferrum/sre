#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
URL="${URL:-http://localhost:30080/}"
DURATION="${DURATION:-45}"
RPS="${RPS:-10}"
KCTX="${KCTX:-kind-hw1}"
K="kubectl --context $KCTX"

echo "== scenario2: load ${RPS} rps for ${DURATION}s, /unready at t=5s, /ready-on at t=25s =="

# background load
./load/loadgen --url "$URL" --rps "$RPS" --duration "${DURATION}s" \
    --out evidence/scenario2_load.csv &
LOAD_PID=$!

sleep 5
POD=$($K get pods -l app=hw1-app -o jsonpath='{.items[0].metadata.name}')
echo "== t=5s: flipping $POD to not-ready =="
$K exec "$POD" -- wget -qO- --post-data="" http://localhost:8080/unready
echo
sleep 6
echo "== t=11s: pod state (expect 0/1, RESTARTS=0) =="
$K get pod "$POD"
echo "== endpoints (expect only the healthy pod) =="
$K get endpoints hw1-app
echo "== endpointslice conditions =="
$K get endpointslices -l kubernetes.io/service-name=hw1-app \
    -o jsonpath='{range .items[*].endpoints[*]}{.addresses} ready={.conditions.ready}{"\n"}{end}'

sleep 9
echo "== t=20s: pod still not-ready, no traffic to it =="
$K get pod "$POD"
echo "== restoring readiness =="
$K exec "$POD" -- wget -qO- --post-data="" http://localhost:8080/ready-on
echo
sleep 5
echo "== t=25s: pod state (expect 1/1, RESTARTS=0) =="
$K get pod "$POD"
echo "== endpoints (expect both pods again) =="
$K get endpoints hw1-app

wait "$LOAD_PID"
echo "== load summary =="
cat evidence/scenario2_load.csv.summary
echo "== requests per pod =="
awk -F, 'NR>1 {print $4}' evidence/scenario2_load.csv | sort | uniq -c
echo "== requests to not-ready pod $POD by second =="
awk -F, -v p="$POD" 'NR>1 && $4==p {print $1}' evidence/scenario2_load.csv | sort | uniq -c
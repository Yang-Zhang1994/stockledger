#!/usr/bin/env bash
# Runs perf/reserve-contention.jmx in Docker against a local stockledger and checks
# the result: every response is 200 or 409, and 200s == units that left stock.
#
#   perf/run-jmeter.sh            # starts the service in -mem mode on :18080
#   STOCK=100 THREADS=50 LOOPS=10 perf/run-jmeter.sh
set -euo pipefail
cd "$(dirname "$0")/.."

PORT="${PORT:-18080}"
STOCK="${STOCK:-100}"
THREADS="${THREADS:-50}"
LOOPS="${LOOPS:-10}"
SKU="JM-$(date +%s)"
OUT="perf/results"
rm -rf "$OUT" && mkdir -p "$OUT"

go build -o "$OUT/stockledger" ./cmd/stockledger
"$OUT/stockledger" -mem -addr ":$PORT" >"$OUT/service.log" 2>&1 &
svc=$!
trap 'kill $svc 2>/dev/null || true' EXIT
until curl -fs "localhost:$PORT/healthz" >/dev/null; do sleep 0.2; done

curl -fsS -X POST "localhost:$PORT/skus/$SKU/receive" -d "{\"qty\":$STOCK}" >/dev/null

# host.docker.internal reaches the host from Docker Desktop; on Linux CI use --network host.
HOST="${JMETER_TARGET_HOST:-host.docker.internal}"
docker run --rm ${JMETER_DOCKER_ARGS:-} -v "$PWD/perf:/perf" justb4/jmeter:5.5 \
  -n -t /perf/reserve-contention.jmx -l /perf/results/results.jtl -j /perf/results/jmeter.log \
  -Jhost="$HOST" -Jport="$PORT" -Jsku="$SKU" -Jthreads="$THREADS" -Jloops="$LOOPS" \
  -Jjmeter.save.saveservice.output_format=csv >/dev/null

# results.jtl columns: timeStamp,elapsed,label,responseCode,responseMessage,threadName,dataType,success,...
total=$(($(wc -l <"$OUT/results.jtl") - 1))
ok=$(awk -F, 'NR>1 && $4=="200"' "$OUT/results.jtl" | wc -l | tr -d ' ')
soldout=$(awk -F, 'NR>1 && $4=="409"' "$OUT/results.jtl" | wc -l | tr -d ' ')
failed=$(awk -F, 'NR>1 && $8!="true"' "$OUT/results.jtl" | wc -l | tr -d ' ')
p95=$(awk -F, 'NR>1 {print $2}' "$OUT/results.jtl" | sort -n | awk '{a[NR]=$1} END {print a[int(NR*0.95)]}')
available=$(curl -fsS "localhost:$PORT/skus/$SKU" | sed -E 's/.*"available":(-?[0-9]+).*/\1/')

echo "requests=$total reserved=$ok sold_out=$soldout assertion_failures=$failed p95_ms=$p95"
echo "stock=$STOCK available_after=$available"

if [ "$failed" -ne 0 ] || [ "$available" -lt 0 ] || [ "$ok" -ne $((STOCK - available)) ]; then
  echo "FAIL: oversold or unexpected responses" >&2
  exit 1
fi
echo "PASS: no oversell under $THREADS concurrent clients"

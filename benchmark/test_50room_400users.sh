#!/bin/bash
# Based on test_10k_connections.sh: 50 rooms x 400 users = 20,000 connections.
# Uses direct connect-node container IPs to avoid Docker published-port SYN backlog bottlenecks.
set -euo pipefail

cd /home/yonhoo/pubsub-refactor/pubsub/benchmark

ROOMS=${ROOMS:-50}
USERS_PER_ROOM=${USERS_PER_ROOM:-400}
TARGET=$((ROOMS * USERS_PER_ROOM))
RUN_ID=${RUN_ID:-$(date +%Y%m%d-%H%M%S)-50room-400users}
ROOM_PREFIX=${ROOM_PREFIX:-room50x400-${RUN_ID}}
USER_PREFIX=${USER_PREFIX:-u50x400-${RUN_ID}}
LOG_DIR=${LOG_DIR:-logs_50room_400users_${RUN_ID}}
JOIN_TIMEOUT=${JOIN_TIMEOUT:-1500}
MAXDELAY=${MAXDELAY:-600}
PUSH_DUR=${PUSH_DUR:-30s}
PUSH_TIMEOUT=${PUSH_TIMEOUT:-60s}
PUSH_ADDR=${PUSH_ADDR:-[::1]:18086}
RATES=${RATES:-"10 20 50 100 200"}
IP1=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' pubsub-connect-node-1)
IP2=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' pubsub-connect-node-2)
IP3=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' pubsub-connect-node-3)
WS=("ws://${IP1}:8083/connect" "ws://${IP2}:8083/connect" "ws://${IP3}:8083/connect")

log() { echo "$*" | tee -a "$LOG_DIR/summary.log"; }
online_count() { docker exec pubsub-redis redis-cli --scan --pattern "user_node:${USER_PREFIX}-*" 2>/dev/null | wc -l | tr -d ' '; }
estab_count() { ss -tan 2>/dev/null | awk '$1=="ESTAB" && ($4 ~ /:1808[3-5]$/ || $5 ~ /:8083$/) {c++} END{print c+0}'; }
alive_sum() { local sum=0 a f; for f in "$LOG_DIR"/s-*.log; do [ -f "$f" ] || continue; a=$(tail -1 "$f" | grep -o "alive:[0-9]*" | cut -d: -f2 || true); a=${a:-0}; sum=$((sum+a)); done; echo "$sum"; }
sample_stats() { docker stats --no-stream --format '{{.Name}}: CPU={{.CPUPerc}} Mem={{.MemUsage}} Net={{.NetIO}}' | grep connect-node | tee -a "$LOG_DIR/summary.log" || true; }
sum_field() {
  local rate="$1" label="$2" sum="0" line v f
  for f in "$LOG_DIR"/push-r${rate}-room*.log; do
    [ -f "$f" ] || continue
    line=$(grep -F "$label" "$f" | tail -1 || true)
    v=$(printf '%s\n' "$line" | sed "s/.*$label[[:space:]]*//; s/[^0-9.].*//")
    [ -n "$v" ] || continue
    sum=$(awk -v a="$sum" -v b="$v" 'BEGIN{printf "%.1f", a+b}')
  done
  printf '%s\n' "$sum"
}

echo "Step 1: optimize local client limits, non-blocking"
ulimit -n 1048576 2>/dev/null || true
if [ "${APPLY_BENCH_SYSCTL:-0}" = "1" ]; then
  sudo -n sysctl -w net.ipv4.ip_local_port_range="10000 65535" >/dev/null 2>&1 || true
  sudo -n sysctl -w net.ipv4.tcp_tw_reuse=1 >/dev/null 2>&1 || true
  sudo -n sysctl -w net.ipv4.tcp_fin_timeout=15 >/dev/null 2>&1 || true
  sudo -n sysctl -w net.ipv4.tcp_rmem="4096 8192 16384" >/dev/null 2>&1 || true
  sudo -n sysctl -w net.ipv4.tcp_wmem="4096 8192 16384" >/dev/null 2>&1 || true
fi

echo "Step 2: check services"
docker ps --format "{{.Names}}: {{.Status}}" | grep -E "connect-node|controller|push-manager|web-server|mysql|redis" || true

echo "Step 3: prepare benchmark"
pkill -9 -x bench_client 2>/dev/null || true
pkill -9 -x multi_push 2>/dev/null || true
sleep 2
rm -rf "$LOG_DIR"
mkdir -p "$LOG_DIR"
: > "$LOG_DIR/summary.log"

log "RUN_ID=$RUN_ID"
log "ROOM_PREFIX=$ROOM_PREFIX"
log "USER_PREFIX=$USER_PREFIX"
log "WS=${WS[*]}"
log "PUSH_ADDR=$PUSH_ADDR"
log "ROOMS=$ROOMS USERS_PER_ROOM=$USERS_PER_ROOM TARGET=$TARGET MAXDELAY=$MAXDELAY JOIN_TIMEOUT=$JOIN_TIMEOUT"

log "Step 4: start ${TARGET} connections (${ROOMS} rooms x ${USERS_PER_ROOM} users)"
for i in $(seq 1 "$ROOMS"); do
  rid=$(printf "%s-%02d" "$ROOM_PREFIX" "$i")
  wi=$((i % 3))
  ./bench_client -room="$rid" -users="$USERS_PER_ROOM" -ws="${WS[$wi]}" -hb=30000 -maxdelay="$MAXDELAY" -log="$LOG_DIR/c-$i.log" -stat="$LOG_DIR/s-$i.log" -prefix="${USER_PREFIX}-$i" >"$LOG_DIR/o-$i.log" 2>&1 &
  if [ $((i % 5)) -eq 0 ]; then log "launched_rooms=$i launched_users=$((i * USERS_PER_ROOM))"; sleep 2; fi
done

log "Step 5: wait for ready target=$TARGET timeout=${JOIN_TIMEOUT}s"
start_ts=$(date +%s)
while true; do
  online=$(online_count)
  alive=$(alive_sum)
  estab=$(estab_count)
  clients=$(pgrep -x bench_client | wc -l | tr -d ' ')
  elapsed=$(( $(date +%s) - start_ts ))
  log "progress elapsed=${elapsed}s redis_user_node=${online:-0} alive=${alive:-0} estab=${estab} bench_clients=${clients}"
  sample_stats
  if [ "${alive:-0}" -ge "$TARGET" ] || [ "${estab:-0}" -ge "$TARGET" ]; then log "join_ready alive=${alive:-0} estab=${estab}"; break; fi
  if [ "$elapsed" -ge "$JOIN_TIMEOUT" ]; then log "join_timeout redis_user_node=${online:-0} alive=${alive:-0} estab=${estab} target=$TARGET"; break; fi
  sleep 10
done

log "FINAL_REDIS_USER_NODE=$(online_count)"
log "FINAL_ALIVE=$(alive_sum)"
log "FINAL_ESTAB=$(estab_count)"
log "BASE_STATS"
sample_stats

log "Step 6: broadcast test, one multi_push per room"
for rate in $RATES; do
  log "=== rate=${rate}/room target_total=$((rate * ROOMS)) msg/s dur=$PUSH_DUR ==="
  rm -f "$LOG_DIR"/push-r${rate}-room*.log
  push_pids=()
  for i in $(seq 1 "$ROOMS"); do
    rid=$(printf "%s-%02d" "$ROOM_PREFIX" "$i")
    env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy \
      timeout "$PUSH_TIMEOUT" ./multi_push -room="$rid" -rate="$rate" -dur="$PUSH_DUR" -addr="$PUSH_ADDR" >"$LOG_DIR/push-r${rate}-room${i}.log" 2>&1 &
    push_pids+=("$!")
  done
  sleep 10
  log "MID_STATS rate=$rate alive=$(alive_sum) estab=$(estab_count)"
  sample_stats
  for pid in "${push_pids[@]}"; do
    wait "$pid" || true
  done
  log "RATE=$rate OK_SUM=$(sum_field "$rate" "成功:") FAIL_SUM=$(sum_field "$rate" "失败:") AVG_RATE_SUM=$(sum_field "$rate" "平均速率:") alive=$(alive_sum)"
done

log "cleanup bench_client / multi_push"
pkill -9 -x bench_client 2>/dev/null || true
pkill -9 -x multi_push 2>/dev/null || true
log "DONE LOG_DIR=$LOG_DIR"
echo "summary: $LOG_DIR/summary.log"

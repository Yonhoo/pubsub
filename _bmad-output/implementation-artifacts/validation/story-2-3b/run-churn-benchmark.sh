#!/usr/bin/env bash
set -euo pipefail

ROOT="${ROOT:-/mnt/pubsub}"
OUT_DIR="${OUT_DIR:-$ROOT/_bmad-output/implementation-artifacts/validation/story-2-3b}"
GO_BIN="${GO_BIN:-go}"
BENCH_REGEX='Bucket(PutChurn|DelChurn|ChangeRoomChurn)Parallel'

mkdir -p "$OUT_DIR"

cd "$ROOT"

"$GO_BIN" test -run='^$' -bench "$BENCH_REGEX" -benchmem \
  -cpuprofile "$OUT_DIR/churn.cpu.pprof" \
  -memprofile "$OUT_DIR/churn.mem.pprof" \
  -mutexprofile "$OUT_DIR/churn.mutex.pprof" \
  -blockprofile "$OUT_DIR/churn.block.pprof" \
  ./connect-node/... | tee "$OUT_DIR/churn.bench.txt"

"$GO_BIN" tool pprof -top "$OUT_DIR/churn.cpu.pprof" | tee "$OUT_DIR/churn.cpu.top.txt"
"$GO_BIN" tool pprof -top -sample_index=alloc_space "$OUT_DIR/churn.mem.pprof" | tee "$OUT_DIR/churn.mem.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/churn.mutex.pprof" | tee "$OUT_DIR/churn.mutex.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/churn.block.pprof" | tee "$OUT_DIR/churn.block.top.txt"

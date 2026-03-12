#!/usr/bin/env bash
set -euo pipefail

ROOT="${ROOT:-/mnt/pubsub}"
OUT_DIR="${OUT_DIR:-$ROOT/_bmad-output/implementation-artifacts/validation/story-2-3a}"
GO_BIN="${GO_BIN:-go}"
BENCH_REGEX='BroadcastSnapshotOptimized'

mkdir -p "$OUT_DIR"

cd "$ROOT"

"$GO_BIN" test -run='^$' -bench "$BENCH_REGEX" -benchmem \
  -cpuprofile "$OUT_DIR/broadcast-opt.cpu.pprof" \
  -memprofile "$OUT_DIR/broadcast-opt.mem.pprof" \
  -mutexprofile "$OUT_DIR/broadcast-opt.mutex.pprof" \
  -blockprofile "$OUT_DIR/broadcast-opt.block.pprof" \
  ./connect-node/... | tee "$OUT_DIR/broadcast-opt.bench.txt"

"$GO_BIN" tool pprof -top "$OUT_DIR/broadcast-opt.cpu.pprof" | tee "$OUT_DIR/broadcast-opt.cpu.top.txt"
"$GO_BIN" tool pprof -top -sample_index=alloc_space "$OUT_DIR/broadcast-opt.mem.pprof" | tee "$OUT_DIR/broadcast-opt.mem.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/broadcast-opt.mutex.pprof" | tee "$OUT_DIR/broadcast-opt.mutex.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/broadcast-opt.block.pprof" | tee "$OUT_DIR/broadcast-opt.block.top.txt"

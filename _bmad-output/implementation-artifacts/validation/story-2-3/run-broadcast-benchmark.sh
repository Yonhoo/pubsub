#!/usr/bin/env bash
set -euo pipefail

ROOT="${ROOT:-/mnt/pubsub}"
OUT_DIR="${OUT_DIR:-$ROOT/_bmad-output/implementation-artifacts/validation/story-2-3}"
GO_BIN="${GO_BIN:-go}"
BENCH_REGEX='BroadcastSnapshot'

mkdir -p "$OUT_DIR"

cd "$ROOT"

"$GO_BIN" test -run='^$' -bench "$BENCH_REGEX" -benchmem \
  -cpuprofile "$OUT_DIR/broadcast.cpu.pprof" \
  -memprofile "$OUT_DIR/broadcast.mem.pprof" \
  -mutexprofile "$OUT_DIR/broadcast.mutex.pprof" \
  -blockprofile "$OUT_DIR/broadcast.block.pprof" \
  ./connect-node/... | tee "$OUT_DIR/broadcast.bench.txt"

"$GO_BIN" tool pprof -top "$OUT_DIR/broadcast.cpu.pprof" | tee "$OUT_DIR/broadcast.cpu.top.txt"
"$GO_BIN" tool pprof -top -sample_index=alloc_space "$OUT_DIR/broadcast.mem.pprof" | tee "$OUT_DIR/broadcast.mem.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/broadcast.mutex.pprof" | tee "$OUT_DIR/broadcast.mutex.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/broadcast.block.pprof" | tee "$OUT_DIR/broadcast.block.top.txt"

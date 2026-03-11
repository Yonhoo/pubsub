#!/usr/bin/env bash
set -euo pipefail

ROOT="${ROOT:-/mnt/pubsub}"
OUT_DIR="${OUT_DIR:-$ROOT/_bmad-output/implementation-artifacts/validation/story-2-2a}"
GO_BIN="${GO_BIN:-go}"
BENCH_REGEX='SharedWriterSteadyStateFlushBy(Count|Bytes|Timeout)'

mkdir -p "$OUT_DIR"

cd "$ROOT"

"$GO_BIN" test -run='^$' -bench "$BENCH_REGEX" -benchmem \
  -cpuprofile "$OUT_DIR/steady.cpu.pprof" \
  -memprofile "$OUT_DIR/steady.mem.pprof" \
  -mutexprofile "$OUT_DIR/steady.mutex.pprof" \
  -blockprofile "$OUT_DIR/steady.block.pprof" \
  ./connect-node/... | tee "$OUT_DIR/steady.bench.txt"

"$GO_BIN" tool pprof -top "$OUT_DIR/steady.cpu.pprof" | tee "$OUT_DIR/steady.cpu.top.txt"
"$GO_BIN" tool pprof -top -sample_index=alloc_space "$OUT_DIR/steady.mem.pprof" | tee "$OUT_DIR/steady.mem.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/steady.mutex.pprof" | tee "$OUT_DIR/steady.mutex.top.txt"
"$GO_BIN" tool pprof -top "$OUT_DIR/steady.block.pprof" | tee "$OUT_DIR/steady.block.top.txt"

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../.." && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}"

GO_BIN="${GO_BIN:-go}"
BENCH_REGEX="${BENCH_REGEX:-DelRoom|Close}"

cd "${REPO_ROOT}"

echo "[baseline] output dir: ${OUTPUT_DIR}"
echo "[baseline] repo root: ${REPO_ROOT}"

"${GO_BIN}" test ./connect-node/... | tee "${OUTPUT_DIR}/go-test.txt"
"${GO_BIN}" test -race ./connect-node/... | tee "${OUTPUT_DIR}/go-test-race.txt"
"${GO_BIN}" test -run='^$' -bench "${BENCH_REGEX}" -benchmem ./connect-node/... | tee "${OUTPUT_DIR}/go-bench.txt"

"${GO_BIN}" test -run='^$' -bench "${BENCH_REGEX}" -benchmem \
  -cpuprofile "${OUTPUT_DIR}/baseline.cpu.pprof" \
  -memprofile "${OUTPUT_DIR}/baseline.mem.pprof" \
  -mutexprofile "${OUTPUT_DIR}/baseline.mutex.pprof" \
  -blockprofile "${OUTPUT_DIR}/baseline.block.pprof" \
  ./connect-node/... | tee "${OUTPUT_DIR}/go-bench-profiled.txt"

"${GO_BIN}" tool pprof -top -nodecount=30 "${OUTPUT_DIR}/baseline.cpu.pprof" > "${OUTPUT_DIR}/baseline.cpu.top.txt"
"${GO_BIN}" tool pprof -top -nodecount=30 "${OUTPUT_DIR}/baseline.mem.pprof" > "${OUTPUT_DIR}/baseline.mem.top.txt"
"${GO_BIN}" tool pprof -top -nodecount=30 "${OUTPUT_DIR}/baseline.mutex.pprof" > "${OUTPUT_DIR}/baseline.mutex.top.txt"
"${GO_BIN}" tool pprof -top -nodecount=30 "${OUTPUT_DIR}/baseline.block.pprof" > "${OUTPUT_DIR}/baseline.block.top.txt"

echo "[baseline] completed"

# Story 1-4 Concurrency Baseline Traceability

## Purpose

本清单将 Epic 1 的并发修复结果映射到可复用的回归入口与验证产物，满足 Story 1.4 对 FR/NFR 可追溯性的要求。

## Reusable Entry

- Script: `/mnt/pubsub/_bmad-output/implementation-artifacts/validation/story-1-4/run-concurrency-baseline.sh`
- Default scope:
  - `go test ./connect-node/...`
  - `go test -race ./connect-node/...`
  - `go test -run='^$' -bench 'DelRoom|Close' -benchmem ./connect-node/...`
  - `pprof` collection for CPU / memory / mutex / block

## Artifact Convention

- `go-test.txt`: functional baseline
- `go-test-race.txt`: race baseline
- `go-bench.txt`: raw benchmark baseline
- `go-bench-profiled.txt`: benchmark run used to generate profiles
- `baseline.cpu.pprof` / `baseline.cpu.top.txt`
- `baseline.mem.pprof` / `baseline.mem.top.txt`
- `baseline.mutex.pprof` / `baseline.mutex.top.txt`
- `baseline.block.pprof` / `baseline.block.top.txt`

## FR / NFR Mapping

| Story | Requirement | Validation Entry | Expected Evidence |
|---|---|---|---|
| 1.1 DelRoom lock semantics | FR10 / NFR7 / NFR9 | `go test -race`, `-bench DelRoom`, mutex/block pprof | 无新增 race；DelRoom lock 路径可追踪 |
| 1.2 non-blocking signal | FR5 / FR7 / FR8 / NFR4 | `go test -race`, signal tests | ready 合并不丢队列请求；signal drop 可读 |
| 1.3 idempotent close | FR6 / NFR5 / NFR8 | `go test -race`, `-bench Close`, CPU/mutex/block pprof | Close 幂等、非阻塞；关闭热点画像可追踪 |
| 1.4 regression pack | FR23 / NFR7 / NFR14 | `run-concurrency-baseline.sh` | 统一产出可追溯通过/失败记录 |

## Pass Criteria

- `go test ./connect-node/...` passes
- `go test -race ./connect-node/...` passes with no new race
- benchmark command completes and outputs baseline metrics
- `pprof` artifacts are generated successfully
- outputs can be traced back to the corresponding FR / NFR rows above

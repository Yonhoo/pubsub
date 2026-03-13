package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadConfigFromFileCapacityParamsFromYAML(t *testing.T) {
	path := writeTempConfig(t, `
shared_writer:
  batch_size: 48
  max_batch_bytes: 131072
  flush_interval: 750ms
  queue_size: 2048
leave_queue:
  retry_delay: 350ms
  max_attempts: 5
`)

	cfg := LoadConfigFromFile(path)
	if cfg.SharedWriter.BatchSize != 48 {
		t.Fatalf("expected batch size 48, got %d", cfg.SharedWriter.BatchSize)
	}
	if cfg.SharedWriter.MaxBatchBytes != 131072 {
		t.Fatalf("expected max batch bytes 131072, got %d", cfg.SharedWriter.MaxBatchBytes)
	}
	if cfg.SharedWriter.FlushInterval != 750*time.Millisecond {
		t.Fatalf("expected flush interval 750ms, got %v", cfg.SharedWriter.FlushInterval)
	}
	if cfg.SharedWriter.QueueSize != 2048 {
		t.Fatalf("expected queue size 2048, got %d", cfg.SharedWriter.QueueSize)
	}
	if cfg.LeaveQueue.RetryDelay != 350*time.Millisecond {
		t.Fatalf("expected retry delay 350ms, got %v", cfg.LeaveQueue.RetryDelay)
	}
	if cfg.LeaveQueue.MaxAttempts != 5 {
		t.Fatalf("expected max attempts 5, got %d", cfg.LeaveQueue.MaxAttempts)
	}
}

func TestLoadConfigFromFileCapacityEnvOverridesYAML(t *testing.T) {
	path := writeTempConfig(t, `
shared_writer:
  batch_size: 48
  queue_size: 2048
leave_queue:
  retry_delay: 350ms
  max_attempts: 5
`)
	t.Setenv("SHARED_WRITER_BATCH_SIZE", "96")
	t.Setenv("SHARED_WRITER_QUEUE_SIZE", "4096")
	t.Setenv("LEAVE_QUEUE_RETRY_DELAY", "900ms")
	t.Setenv("LEAVE_QUEUE_MAX_ATTEMPTS", "7")

	cfg := LoadConfigFromFile(path)
	if cfg.SharedWriter.BatchSize != 96 {
		t.Fatalf("expected env batch size 96, got %d", cfg.SharedWriter.BatchSize)
	}
	if cfg.SharedWriter.QueueSize != 4096 {
		t.Fatalf("expected env queue size 4096, got %d", cfg.SharedWriter.QueueSize)
	}
	if cfg.LeaveQueue.RetryDelay != 900*time.Millisecond {
		t.Fatalf("expected env retry delay 900ms, got %v", cfg.LeaveQueue.RetryDelay)
	}
	if cfg.LeaveQueue.MaxAttempts != 7 {
		t.Fatalf("expected env max attempts 7, got %d", cfg.LeaveQueue.MaxAttempts)
	}
}

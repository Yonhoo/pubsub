package main

import "testing"

func TestLoadProfilingSettingsDefaultsDisabled(t *testing.T) {
	t.Setenv("CONNECT_NODE_PPROF_ENABLED", "")
	t.Setenv("CONNECT_NODE_PPROF_PORT", "")
	t.Setenv("CONNECT_NODE_MUTEX_PROFILE_FRACTION", "")
	t.Setenv("CONNECT_NODE_BLOCK_PROFILE_RATE", "")
	t.Setenv("CONNECT_NODE_WRITE_TRACE_LOG", "")

	settings := loadProfilingSettingsFromEnv()
	if settings.pprofEnabled {
		t.Fatalf("expected pprof to be disabled by default")
	}
	if settings.pprofPort != 6060 {
		t.Fatalf("expected default pprof port 6060, got %d", settings.pprofPort)
	}
	if settings.mutexProfileFraction != 0 {
		t.Fatalf("expected default mutex profile fraction 0, got %d", settings.mutexProfileFraction)
	}
	if settings.blockProfileRate != 0 {
		t.Fatalf("expected default block profile rate 0, got %d", settings.blockProfileRate)
	}
	if settings.writeTraceLog {
		t.Fatalf("expected write trace log to be disabled by default")
	}
}

func TestLoadProfilingSettingsEnvOverrides(t *testing.T) {
	t.Setenv("CONNECT_NODE_PPROF_ENABLED", "1")
	t.Setenv("CONNECT_NODE_PPROF_PORT", "7070")
	t.Setenv("CONNECT_NODE_MUTEX_PROFILE_FRACTION", "7")
	t.Setenv("CONNECT_NODE_BLOCK_PROFILE_RATE", "250000")
	t.Setenv("CONNECT_NODE_WRITE_TRACE_LOG", "1")

	settings := loadProfilingSettingsFromEnv()
	if !settings.pprofEnabled {
		t.Fatalf("expected pprof to be enabled")
	}
	if settings.pprofPort != 7070 {
		t.Fatalf("expected pprof port 7070, got %d", settings.pprofPort)
	}
	if settings.mutexProfileFraction != 7 {
		t.Fatalf("expected mutex profile fraction 7, got %d", settings.mutexProfileFraction)
	}
	if settings.blockProfileRate != 250000 {
		t.Fatalf("expected block profile rate 250000, got %d", settings.blockProfileRate)
	}
	if !settings.writeTraceLog {
		t.Fatalf("expected write trace log to be enabled")
	}
}

package main

import (
	"log"
	"os"
	"runtime"
	"strconv"
)

type profilingSettings struct {
	pprofEnabled         bool
	pprofPort            int
	mutexProfileFraction int
	blockProfileRate     int
	writeTraceLog        bool
}

func loadProfilingSettingsFromEnv() profilingSettings {
	settings := profilingSettings{
		pprofEnabled:         envBool("CONNECT_NODE_PPROF_ENABLED", false),
		pprofPort:            envInt("CONNECT_NODE_PPROF_PORT", 6060),
		mutexProfileFraction: envInt("CONNECT_NODE_MUTEX_PROFILE_FRACTION", 0),
		blockProfileRate:     envInt("CONNECT_NODE_BLOCK_PROFILE_RATE", 0),
		writeTraceLog:        envBool("CONNECT_NODE_WRITE_TRACE_LOG", false),
	}

	if settings.pprofPort <= 0 {
		settings.pprofPort = 6060
	}
	if settings.mutexProfileFraction < 0 {
		settings.mutexProfileFraction = 0
	}
	if settings.blockProfileRate < 0 {
		settings.blockProfileRate = 0
	}

	return settings
}

func applyProfilingSettings(settings profilingSettings) {
	runtime.SetMutexProfileFraction(settings.mutexProfileFraction)
	runtime.SetBlockProfileRate(settings.blockProfileRate)
	log.Printf("🔬 Profiling settings: pprof_enabled=%t pprof_port=%d mutex_fraction=%d block_rate=%d write_trace=%t\n",
		settings.pprofEnabled,
		settings.pprofPort,
		settings.mutexProfileFraction,
		settings.blockProfileRate,
		settings.writeTraceLog,
	)
}

func envBool(key string, defaultValue bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return defaultValue
	}
	return value == "1" || value == "true" || value == "TRUE" || value == "True"
}

func envInt(key string, defaultValue int) int {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

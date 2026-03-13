package main

import (
	"context"
	"sync"
	"time"

	pkgmetrics "github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
)

var (
	criticalMetricsMu sync.RWMutex
	criticalMetrics   *pkgmetrics.MetricsCollector
)

func setCriticalMetricsCollector(mc *pkgmetrics.MetricsCollector) {
	criticalMetricsMu.Lock()
	criticalMetrics = mc
	criticalMetricsMu.Unlock()
}

func getCriticalMetricsCollector() *pkgmetrics.MetricsCollector {
	criticalMetricsMu.RLock()
	defer criticalMetricsMu.RUnlock()
	return criticalMetrics
}

func recordCriticalDrop(kind, reason string) {
	if mc := getCriticalMetricsCollector(); mc != nil {
		mc.RecordCriticalDrop(context.Background(), kind, reason)
	}
}

func recordCriticalEnqueueFailure(source, reason string) {
	if mc := getCriticalMetricsCollector(); mc != nil {
		mc.RecordCriticalEnqueueFailure(context.Background(), source, reason)
	}
}

func recordCriticalLockBlock(scope, op string, duration time.Duration) {
	if mc := getCriticalMetricsCollector(); mc != nil {
		mc.RecordCriticalLockBlock(context.Background(), scope, op, duration)
	}
}

func recordCriticalCloseLatency(path, result string, duration time.Duration) {
	if mc := getCriticalMetricsCollector(); mc != nil {
		// NOTE: this records local close-path cleanup latency only.
		// It does not represent end-to-end LeaveRoom completion latency.
		mc.RecordCriticalCloseLatency(context.Background(), path, result, duration)
	}
}

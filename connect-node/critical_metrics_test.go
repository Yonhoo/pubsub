package main

import (
	"testing"

	pkgmetrics "github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
)

func TestCriticalMetricHooksAreNilSafe(t *testing.T) {
	setCriticalMetricsCollector(nil)
	recordCriticalDrop("push", "queue_full")
	recordCriticalEnqueueFailure("response", "queue_full")
	recordCriticalLockBlock("bucket", "put", 0)
	recordCriticalCloseLatency("cleanup_user", "success", 0)
	recordCriticalShutdownDrain("leave_queue", "completed", 1)
}

func TestCriticalCloseCleanupMetricNameIsStable(t *testing.T) {
	const expected = "pubsub.critical.close_cleanup.duration"
	if pkgmetrics.MetricNameCriticalCloseCleanupDur != expected {
		t.Fatalf("critical close cleanup metric name changed: want %q got %q", expected, pkgmetrics.MetricNameCriticalCloseCleanupDur)
	}
}

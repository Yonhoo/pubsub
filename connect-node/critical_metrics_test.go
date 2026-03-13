package main

import "testing"

func TestCriticalMetricHooksAreNilSafe(t *testing.T) {
	setCriticalMetricsCollector(nil)
	recordCriticalDrop("push", "queue_full")
	recordCriticalEnqueueFailure("response", "queue_full")
	recordCriticalLockBlock("bucket", "put", 0)
	recordCriticalCloseLatency("cleanup_user", "success", 0)
}

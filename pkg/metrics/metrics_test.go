package metrics

import (
	"context"
	"testing"
	"time"
)

func TestCriticalMetricsRecordersDoNotPanic(t *testing.T) {
	mc, err := NewMetricsCollector("node-1", "connect-node")
	if err != nil {
		t.Fatalf("NewMetricsCollector() error = %v", err)
	}
	ctx := context.Background()
	mc.RecordCriticalDrop(ctx, "push", "queue_full")
	mc.RecordCriticalEnqueueFailure(ctx, "response", "queue_full")
	mc.RecordCriticalLockBlock(ctx, "bucket", "put", 12*time.Millisecond)
	mc.RecordCriticalCloseLatency(ctx, "cleanup_user", "success", 3*time.Millisecond)
}

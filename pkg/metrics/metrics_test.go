package metrics

import (
	"context"
	"reflect"
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

func TestCriticalMetricSpecsStable(t *testing.T) {
	got := CriticalMetricSpecs()
	want := []MetricSpec{
		{Name: MetricNameSharedWriterEnqueueTotal, LabelKeys: []string{"source", "result", "reason"}},
		{Name: MetricNameLeaveTotal, LabelKeys: []string{"result", "reason"}},
		{Name: MetricNameCriticalDropTotal, LabelKeys: []string{"kind", "reason"}},
		{Name: MetricNameCriticalEnqueueFailTotal, LabelKeys: []string{"source", "reason"}},
		{Name: MetricNameCriticalLockBlockDur, LabelKeys: []string{"scope", "op"}},
		{Name: MetricNameCriticalCloseCleanupDur, LabelKeys: []string{"path", "result"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CriticalMetricSpecs() mismatch:\nwant=%#v\ngot=%#v", want, got)
	}
}

func TestCriticalMetricSpecsUniqueNames(t *testing.T) {
	specs := CriticalMetricSpecs()
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Name == "" {
			t.Fatal("metric name must not be empty")
		}
		if len(spec.LabelKeys) == 0 {
			t.Fatalf("metric %s has empty label set", spec.Name)
		}
		if _, ok := seen[spec.Name]; ok {
			t.Fatalf("duplicate metric name: %s", spec.Name)
		}
		seen[spec.Name] = struct{}{}
	}
}

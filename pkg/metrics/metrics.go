// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsCollector 指标收集器
type MetricsCollector struct {
	meter metric.Meter

	// Service info
	serviceName string
	serviceID   string

	// Controller metrics
	totalRooms                   metric.Int64UpDownCounter
	totalUsers                   metric.Int64UpDownCounter
	totalNodes                   metric.Int64UpDownCounter
	roomUserCount                metric.Int64ObservableGauge
	apiRequestCount              metric.Int64Counter
	apiErrorCount                metric.Int64Counter
	sharedWriterEnqueueTotal     metric.Int64Counter
	leaveOutcomeTotal            metric.Int64Counter
	criticalDropTotal            metric.Int64Counter
	criticalEnqueueFailTotal     metric.Int64Counter
	criticalLockBlockDuration    metric.Float64Histogram
	criticalCloseCleanupDuration metric.Float64Histogram
	nodeConnections              metric.Int64ObservableGauge

	// 用于计算当前值
	mu                 sync.RWMutex
	currentRooms       int64
	currentUsers       int64
	currentNodes       int64
	roomUsers          map[string]int64
	nodeConnectionsMap map[string]int64
}

type MetricSpec struct {
	Name      string
	LabelKeys []string
}

const (
	MetricNameSharedWriterEnqueueTotal = "pubsub.shared_writer.enqueue.total"
	MetricNameLeaveTotal               = "pubsub.leave.total"
	MetricNameCriticalDropTotal        = "pubsub.critical.drop.total"
	MetricNameCriticalEnqueueFailTotal = "pubsub.critical.enqueue_fail.total"
	MetricNameCriticalLockBlockDur     = "pubsub.critical.lock_block.duration"
	MetricNameCriticalCloseCleanupDur  = "pubsub.critical.close_cleanup.duration"
)

var criticalMetricSpecs = []MetricSpec{
	{Name: MetricNameSharedWriterEnqueueTotal, LabelKeys: []string{"source", "result", "reason"}},
	{Name: MetricNameLeaveTotal, LabelKeys: []string{"result", "reason"}},
	{Name: MetricNameCriticalDropTotal, LabelKeys: []string{"kind", "reason"}},
	{Name: MetricNameCriticalEnqueueFailTotal, LabelKeys: []string{"source", "reason"}},
	{Name: MetricNameCriticalLockBlockDur, LabelKeys: []string{"scope", "op"}},
	{Name: MetricNameCriticalCloseCleanupDur, LabelKeys: []string{"path", "result"}},
}

func CriticalMetricSpecs() []MetricSpec {
	out := make([]MetricSpec, 0, len(criticalMetricSpecs))
	for _, spec := range criticalMetricSpecs {
		cp := MetricSpec{
			Name:      spec.Name,
			LabelKeys: append([]string(nil), spec.LabelKeys...),
		}
		out = append(out, cp)
	}
	return out
}

// NewMetricsCollector 创建指标收集器
func NewMetricsCollector(serviceID, serviceName string) (*MetricsCollector, error) {
	meter := otel.Meter(serviceName)

	mc := &MetricsCollector{
		meter:              meter,
		serviceName:        serviceName,
		serviceID:          serviceID,
		roomUsers:          make(map[string]int64),
		nodeConnectionsMap: make(map[string]int64),
	}

	var err error

	// 房间数量
	mc.totalRooms, err = meter.Int64UpDownCounter(
		"pubsub.rooms.total",
		metric.WithDescription("Total number of rooms"),
		metric.WithUnit("{room}"),
	)
	if err != nil {
		return nil, err
	}

	// 用户数量
	mc.totalUsers, err = meter.Int64UpDownCounter(
		"pubsub.users.total",
		metric.WithDescription("Total number of online users"),
		metric.WithUnit("{user}"),
	)
	if err != nil {
		return nil, err
	}

	// 节点数量
	mc.totalNodes, err = meter.Int64UpDownCounter(
		"pubsub.nodes.total",
		metric.WithDescription("Total number of connect nodes"),
		metric.WithUnit("{node}"),
	)
	if err != nil {
		return nil, err
	}

	// API 请求计数
	mc.apiRequestCount, err = meter.Int64Counter(
		"pubsub.api.requests.total",
		metric.WithDescription("Total number of API requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	// API 错误计数
	mc.apiErrorCount, _ = meter.Int64Counter(
		"pubsub.api.errors.total",
		metric.WithDescription("Total number of API errors"),
		metric.WithUnit("{error}"),
	)

	mc.sharedWriterEnqueueTotal, err = meter.Int64Counter(
		MetricNameSharedWriterEnqueueTotal,
		metric.WithDescription("Shared writer enqueue attempts by source/result/reason"),
		metric.WithUnit("{enqueue}"),
	)
	if err != nil {
		return nil, err
	}

	mc.leaveOutcomeTotal, err = meter.Int64Counter(
		MetricNameLeaveTotal,
		metric.WithDescription("Leave queue outcomes by result and reason"),
		metric.WithUnit("{leave}"),
	)
	if err != nil {
		return nil, err
	}

	mc.criticalDropTotal, err = meter.Int64Counter(
		MetricNameCriticalDropTotal,
		metric.WithDescription("Critical drop events by kind and reason"),
		metric.WithUnit("{drop}"),
	)
	if err != nil {
		return nil, err
	}

	mc.criticalEnqueueFailTotal, err = meter.Int64Counter(
		MetricNameCriticalEnqueueFailTotal,
		metric.WithDescription("Critical enqueue failures by source and reason"),
		metric.WithUnit("{failure}"),
	)
	if err != nil {
		return nil, err
	}

	mc.criticalLockBlockDuration, err = meter.Float64Histogram(
		MetricNameCriticalLockBlockDur,
		metric.WithDescription("Lock wait duration for critical operations"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	mc.criticalCloseCleanupDuration, err = meter.Float64Histogram(
		MetricNameCriticalCloseCleanupDur,
		metric.WithDescription("Local session close cleanup latency (includes local detach and leave enqueue only; not end-to-end leave completion)"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	return mc, nil
}

// ========== Room Metrics ==========

// IncrementRooms 增加房间数
func (m *MetricsCollector) IncrementRooms(ctx context.Context, count int64) {
	m.mu.Lock()
	m.currentRooms += count
	m.mu.Unlock()
	m.totalRooms.Add(ctx, count)
}

// DecrementRooms 减少房间数
func (m *MetricsCollector) DecrementRooms(ctx context.Context, count int64) {
	m.mu.Lock()
	m.currentRooms -= count
	m.mu.Unlock()
	m.totalRooms.Add(ctx, -count)
}

// SetRoomUserCount 设置房间用户数
func (m *MetricsCollector) SetRoomUserCount(roomID string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roomUsers[roomID] = count
}

// RemoveRoom 移除房间指标
func (m *MetricsCollector) RemoveRoom(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.roomUsers, roomID)
}

// ========== User Metrics ==========

// IncrementUsers 增加用户数
func (m *MetricsCollector) IncrementUsers(ctx context.Context, count int64) {
	m.mu.Lock()
	m.currentUsers += count
	m.mu.Unlock()
	m.totalUsers.Add(ctx, count)
}

// DecrementUsers 减少用户数
func (m *MetricsCollector) DecrementUsers(ctx context.Context, count int64) {
	m.mu.Lock()
	m.currentUsers -= count
	m.mu.Unlock()
	m.totalUsers.Add(ctx, -count)
}

// ========== Node Metrics ==========

// IncrementNodes 增加节点数
func (m *MetricsCollector) IncrementNodes(ctx context.Context) {
	m.mu.Lock()
	m.currentNodes++
	m.mu.Unlock()
	m.totalNodes.Add(ctx, 1)
}

// DecrementNodes 减少节点数
func (m *MetricsCollector) DecrementNodes(ctx context.Context) {
	m.mu.Lock()
	m.currentNodes--
	m.mu.Unlock()
	m.totalNodes.Add(ctx, -1)
}

// ========== API Metrics ==========

// RecordAPIRequest 记录 API 请求
func (m *MetricsCollector) RecordAPIRequest(ctx context.Context, method string, success bool) {
	attrs := []attribute.KeyValue{
		attribute.String("method", method),
		attribute.Bool("success", success),
	}

	m.apiRequestCount.Add(ctx, 1, metric.WithAttributes(attrs...))

	if !success {
		m.apiErrorCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func (m *MetricsCollector) RecordSharedWriterEnqueue(ctx context.Context, source, result, reason string) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("source", source),
		attribute.String("result", result),
		attribute.String("reason", reason),
	}
	m.sharedWriterEnqueueTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (m *MetricsCollector) RecordLeaveOutcome(ctx context.Context, result, reason string) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("result", result),
		attribute.String("reason", reason),
	}
	m.leaveOutcomeTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (m *MetricsCollector) RecordCriticalDrop(ctx context.Context, kind, reason string) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("kind", kind),
		attribute.String("reason", reason),
	}
	m.criticalDropTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (m *MetricsCollector) RecordCriticalEnqueueFailure(ctx context.Context, source, reason string) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("source", source),
		attribute.String("reason", reason),
	}
	m.criticalEnqueueFailTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func (m *MetricsCollector) RecordCriticalLockBlock(ctx context.Context, scope, op string, duration time.Duration) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("scope", scope),
		attribute.String("op", op),
	}
	m.criticalLockBlockDuration.Record(ctx, float64(duration.Microseconds())/1000.0, metric.WithAttributes(attrs...))
}

func (m *MetricsCollector) RecordCriticalCloseLatency(ctx context.Context, path, result string, duration time.Duration) {
	if m == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("path", path),
		attribute.String("result", result),
	}
	m.criticalCloseCleanupDuration.Record(ctx, float64(duration.Microseconds())/1000.0, metric.WithAttributes(attrs...))
}

// ========== Getters ==========

// GetCurrentRooms 获取当前房间数
func (m *MetricsCollector) GetCurrentRooms() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentRooms
}

// GetCurrentUsers 获取当前用户数
func (m *MetricsCollector) GetCurrentUsers() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentUsers
}

// GetCurrentNodes 获取当前节点数
func (m *MetricsCollector) GetCurrentNodes() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentNodes
}

// SetNodeConnections 设置节点连接数
func (m *MetricsCollector) SetNodeConnections(nodeID string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeConnectionsMap[nodeID] = count
}

// Handler 返回 Prometheus metrics HTTP handler
func (m *MetricsCollector) Handler() http.Handler {
	return promhttp.Handler()
}

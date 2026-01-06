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
	totalRooms      metric.Int64UpDownCounter
	totalUsers      metric.Int64UpDownCounter
	totalNodes      metric.Int64UpDownCounter
	roomUserCount   metric.Int64ObservableGauge
	apiRequestCount metric.Int64Counter
	apiErrorCount   metric.Int64Counter
	nodeConnections metric.Int64ObservableGauge

	// 用于计算当前值
	mu                 sync.RWMutex
	currentRooms       int64
	currentUsers       int64
	currentNodes       int64
	roomUsers          map[string]int64
	nodeConnectionsMap map[string]int64
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

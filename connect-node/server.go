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

package main

import (
	"context"
	"errors"
	getty "github.com/AlexStocks/getty/transport"
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/protocol/controller"
	"github.com/livekit/psrpc/examples/pubsub/protocol/push"
	"github.com/zhenjl/cityhash"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"sync"
	"time"

	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
	"github.com/livekit/psrpc/examples/pubsub/pkg/metrics"
)

// ConnectNodeServer 连接节点服务器
type ConnectNodeServer struct {
	push.UnimplementedCometServer

	// 基础配置
	nodeID      string
	nodeAddress string
	config      *config.Config

	// gRPC 客户端（用于调用 Controller）
	controllerClient controller.ControllerServiceClient

	// Metrics
	metrics *metrics.MetricsCollector

	// accept round store
	round *Round

	sessionMap map[*getty.Session]*clientProtoSession

	buckets []*Bucket

	bucketIdx uint32

	// 房间同步停止信号
	stopRoomSync chan struct{}

	// Shared writer for websocket session flush.
	sharedWriter *sharedWriteManager

	// Shared room broadcast workers (global pool).
	roomWorkers              []chan *push.BroadcastRoomReq
	roomWorkerNum            uint32
	roomWorkerEnqueueTimeout time.Duration
	leaveQueue               chan *leaveTask
	leaveWorkerNum           uint32
	leaveRetryDelay          time.Duration
	leaveMaxAttempts         int
	leavePendingMu           sync.Mutex
	leavePending             map[string]struct{}
	stopOnce                 sync.Once
}

type leaveTask struct {
	key        string
	userID     string
	roomID     string
	attempts   int
	enqueuedAt time.Time
}

// NewConnectNodeServer 创建连接节点服务器
func NewConnectNodeServer(
	nodeID, nodeAddress string,
	cfg *config.Config,
	controllerClient controller.ControllerServiceClient,
	metricsCollector *metrics.MetricsCollector,
) *ConnectNodeServer {
	// 优先使用传入的 nodeID（来自环境变量 NODE_ID），如果为空则使用配置中的 Server.ID
	// 注意：传入的 nodeID 应该是正确的（来自环境变量），不应该被覆盖
	finalNodeID := nodeID
	if finalNodeID == "" {
		if cfg.Server.ID != "" {
			finalNodeID = cfg.Server.ID
			log.Printf("⚠️  [ConnectNodeServer] 传入的 nodeID 为空，使用配置的 Server.ID: %s", finalNodeID)
		} else {
			log.Printf("❌ [ConnectNodeServer] nodeID 和 Server.ID 都为空！这会导致 JoinRoom 失败！")
		}
	} else {
		log.Printf("✅ [ConnectNodeServer] 使用传入的 nodeID: %s", finalNodeID)
	}

	if finalNodeID == "" {
		log.Printf("❌ [ConnectNodeServer] 最终 nodeID 为空！这会导致 JoinRoom 失败！")
		finalNodeID = "connect-node-1" // 最后的兜底，避免崩溃
		log.Printf("⚠️  [ConnectNodeServer] 使用默认 nodeID: %s", finalNodeID)
	}

	roomWorkerEnqueueTimeout := cfg.Bucket.RoutineBackpressureTimeout
	if roomWorkerEnqueueTimeout <= 0 {
		roomWorkerEnqueueTimeout = 200 * time.Millisecond
	}

	server := &ConnectNodeServer{
		nodeID:                   finalNodeID, // 使用确定后的 nodeID
		nodeAddress:              nodeAddress,
		config:                   cfg,
		controllerClient:         controllerClient,
		metrics:                  metricsCollector,
		buckets:                  make([]*Bucket, cfg.Bucket.Size),
		bucketIdx:                uint32(cfg.Bucket.Size),
		round:                    NewRound(cfg),
		roomWorkerEnqueueTimeout: roomWorkerEnqueueTimeout,
		leaveRetryDelay:          200 * time.Millisecond,
		leaveMaxAttempts:         3,
		leavePending:             make(map[string]struct{}),
		stopRoomSync:             make(chan struct{}),
	}

	server.sharedWriter = newSharedWriteManager(
		0,
		writeBatchSize,
		writeBatchMaxBytes,
		writeBatchTimeout,
		writeBatchQueueSize,
	)
	server.sharedWriter.Start()

	for i := 0; i < cfg.Bucket.Size; i++ {
		server.buckets[i] = NewBucket(cfg.Bucket)
	}
	server.initRoomWorkers(cfg.Bucket.RoutineAmount, cfg.Bucket.RoutineSize)
	server.initLeaveWorkers(cfg.Bucket.RoutineAmount, cfg.Bucket.RoutineSize)

	go server.onlineproc()

	return server
}

func (s *ConnectNodeServer) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.leaveQueue != nil {
			close(s.leaveQueue)
			s.leaveQueue = nil
			s.leaveWorkerNum = 0
		}
		for _, ch := range s.roomWorkers {
			if ch != nil {
				close(ch)
			}
		}
		s.roomWorkers = nil
		s.roomWorkerNum = 0
		if s.sharedWriter != nil {
			s.sharedWriter.Stop()
		}
	})
}

func (s *ConnectNodeServer) initRoomWorkers(workerNum uint64, queueSize int) {
	if workerNum == 0 {
		workerNum = 1
	}
	if queueSize <= 0 {
		queueSize = 1024
	}

	s.roomWorkers = make([]chan *push.BroadcastRoomReq, workerNum)
	s.roomWorkerNum = uint32(workerNum)
	for i := uint64(0); i < workerNum; i++ {
		ch := make(chan *push.BroadcastRoomReq, queueSize)
		s.roomWorkers[i] = ch
		go s.roomBroadcastProc(ch)
	}
}

func leaveDedupKey(roomID, userID string) string {
	return roomID + ":" + userID
}

func (s *ConnectNodeServer) initLeaveWorkers(workerNum uint64, queueSize int) {
	if workerNum == 0 {
		workerNum = 1
	}
	if queueSize <= 0 {
		queueSize = 1024
	}
	s.leaveQueue = make(chan *leaveTask, queueSize)
	s.leaveWorkerNum = uint32(workerNum)
	for i := uint64(0); i < workerNum; i++ {
		go s.leaveWorkerProc(s.leaveQueue)
	}
}

func (s *ConnectNodeServer) leaveWorkerProc(ch <-chan *leaveTask) {
	for task := range ch {
		if task == nil {
			continue
		}
		s.processLeaveTask(task)
	}
}

func (s *ConnectNodeServer) EnqueueLeave(userID, roomID string) error {
	if s == nil || userID == "" || roomID == "" {
		return nil
	}
	key := leaveDedupKey(roomID, userID)
	task := &leaveTask{
		key:        key,
		userID:     userID,
		roomID:     roomID,
		enqueuedAt: time.Now(),
	}
	return s.enqueueLeaveTask(task, true)
}

func (s *ConnectNodeServer) enqueueLeaveTask(task *leaveTask, markPending bool) error {
	if s == nil || task == nil || task.userID == "" || task.roomID == "" {
		return nil
	}
	if markPending {
		key := task.key
		if key == "" {
			key = leaveDedupKey(task.roomID, task.userID)
			task.key = key
		}
		s.leavePendingMu.Lock()
		if _, ok := s.leavePending[key]; ok {
			s.leavePendingMu.Unlock()
			return nil
		}
		s.leavePending[key] = struct{}{}
		s.leavePendingMu.Unlock()
	}

	var (
		enqueueCtx context.Context
		cancel     context.CancelFunc
	)
	if s.roomWorkerEnqueueTimeout > 0 {
		enqueueCtx, cancel = context.WithTimeout(context.Background(), s.roomWorkerEnqueueTimeout)
		defer cancel()
	} else {
		enqueueCtx = context.Background()
	}

	select {
	case s.leaveQueue <- task:
		return nil
	case <-enqueueCtx.Done():
		if markPending {
			s.leavePendingMu.Lock()
			delete(s.leavePending, task.key)
			s.leavePendingMu.Unlock()
		}
		return enqueueCtx.Err()
	}
}

func (s *ConnectNodeServer) processLeaveTask(task *leaveTask) {
	finish := func() {
		s.leavePendingMu.Lock()
		delete(s.leavePending, task.key)
		s.leavePendingMu.Unlock()
	}
	if s.controllerClient == nil {
		wsLog("⚠️  [LeaveQueue] controller client unavailable user=%s room=%s", task.userID, task.roomID)
		if s.metrics != nil {
			s.metrics.RecordLeaveOutcome(context.Background(), "final_failure", "controller_unavailable")
		}
		finish()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startTime := time.Now()
	resp, err := s.controllerClient.LeaveRoom(ctx, &controller.LeaveRoomRequest{
		UserId: task.userID,
		RoomId: task.roomID,
	}, grpc.WaitForReady(true))
	elapsed := time.Since(startTime)
	task.attempts++

	if err != nil {
		if s.leaveMaxAttempts <= 0 {
			s.leaveMaxAttempts = 1
		}
		if task.attempts < s.leaveMaxAttempts {
			if s.metrics != nil {
				s.metrics.RecordLeaveOutcome(context.Background(), "retry_scheduled", "rpc_error")
			}
			wsLog("⚠️  [LeaveQueue] LeaveRoom 重试排队 user=%s room=%s attempts=%d elapsed=%v err=%v",
				task.userID, task.roomID, task.attempts, elapsed, err)
			s.scheduleLeaveRetry(task)
			return
		}
		wsLog("❌ [LeaveQueue] LeaveRoom 失败 user=%s room=%s attempts=%d elapsed=%v err=%v",
			task.userID, task.roomID, task.attempts, elapsed, err)
		wsLog("🚨 [LeaveQueue] LeaveRoom 最终失败 user=%s room=%s attempts=%d err=%v",
			task.userID, task.roomID, task.attempts, err)
		if s.metrics != nil {
			s.metrics.RecordLeaveOutcome(context.Background(), "final_failure", "rpc_error")
		}
		finish()
		return
	}
	if resp != nil {
		wsLog("✅ [LeaveQueue] LeaveRoom 成功 user=%s room=%s attempts=%d elapsed=%v",
			task.userID, task.roomID, task.attempts, elapsed)
		if s.metrics != nil {
			s.metrics.RecordLeaveOutcome(context.Background(), "success", "none")
		}
		finish()
		return
	}
	wsLog("⚠️  [LeaveQueue] LeaveRoom 返回 nil user=%s room=%s attempts=%d elapsed=%v",
		task.userID, task.roomID, task.attempts, elapsed)
	if s.metrics != nil {
		s.metrics.RecordLeaveOutcome(context.Background(), "final_failure", "nil_response")
	}
	finish()
}

func (s *ConnectNodeServer) scheduleLeaveRetry(task *leaveTask) {
	if s == nil || task == nil {
		return
	}
	delay := s.leaveRetryDelay
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}
	go func(t *leaveTask) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
		if err := s.enqueueLeaveTask(t, false); err != nil {
			wsLog("🚨 [LeaveQueue] LeaveRoom 重试入队失败 user=%s room=%s attempts=%d err=%v",
				t.userID, t.roomID, t.attempts, err)
			if s.metrics != nil {
				s.metrics.RecordLeaveOutcome(context.Background(), "final_failure", "retry_enqueue")
			}
			s.leavePendingMu.Lock()
			delete(s.leavePending, t.key)
			s.leavePendingMu.Unlock()
		}
	}(task)
}

func (s *ConnectNodeServer) roomBroadcastProc(ch chan *push.BroadcastRoomReq) {
	for req := range ch {
		if req == nil || req.Proto == nil || req.RoomID == "" {
			continue
		}
		for _, bucket := range s.Buckets() {
			if room := bucket.Room(req.RoomID); room != nil {
				room.PushMsg(req.Proto)
			}
		}
	}
}

func (s *ConnectNodeServer) Buckets() []*Bucket {
	return s.buckets
}

func (s *ConnectNodeServer) Bucket(clientID string) *Bucket {

	idx := cityhash.CityHash32([]byte(clientID), uint32(len(clientID))) % s.bucketIdx

	return s.buckets[idx]
}

func (s *ConnectNodeServer) onlineproc() {
	for {
		var (
			allRoomsCount map[string]int32
			//err           error
		)

		roomCount := make(map[string]int32)

		for _, bucket := range s.buckets {
			for roomID, count := range bucket.RoomsCount() {
				roomCount[roomID] += count
			}
		}

		for _, bucket := range s.buckets {
			bucket.UpRoomsCount(allRoomsCount)
		}

		time.Sleep(time.Second * 10)
	}

}

// ========== RPC 方法实现 ==========

// 假设 userId 是全局 服务端颁发
func (s *ConnectNodeServer) PushMsg(ctx context.Context, req *push.PushMsgReq) (reply *push.PushMsgReply, err error) {
	if len(req.Keys) == 0 || req.Proto == nil {
		return nil, pkg.ErrPushMsgArg
	}

	for _, key := range req.Keys {
		bucket := s.Bucket(key)

		if bucket == nil {
			continue
		}

		if channel := bucket.Channel(key); channel != nil {
			if !channel.NeedPush(req.ProtoOp) {
				continue
			}

			if err = channel.Push(req.Proto); err != nil {
				return
			}
		}

	}

	return &push.PushMsgReply{}, nil

}

func (s *ConnectNodeServer) Broadcast(ctx context.Context, req *push.BroadcastReq) (*push.BroadcastReply, error) {
	if req.Proto == nil {
		return nil, pkg.ErrBroadCastArg
	}

	go func() {
		for _, bucket := range s.Buckets() {
			bucket.Broadcast(req.GetProto(), req.ProtoOp)
			if req.Speed > 0 {
				t := bucket.ChannelCount() / int(req.Speed)
				time.Sleep(time.Duration(t) * time.Second)
			}
		}
	}()
	return &push.BroadcastReply{}, nil
}

func (s *ConnectNodeServer) BroadcastRoom(ctx context.Context, req *push.BroadcastRoomReq) (*push.BroadcastRoomReply, error) {
	if req.Proto == nil || req.RoomID == "" {
		log.Printf("[ConnectNodeServer] invalid args: roomID=%s, proto=%v", req.RoomID, req.Proto)
		return nil, pkg.ErrBroadCastRoomArg
	}
	if len(s.roomWorkers) == 0 || s.roomWorkerNum == 0 {
		for _, bucket := range s.Buckets() {
			if room := bucket.Room(req.RoomID); room != nil {
				room.PushMsg(req.Proto)
			}
		}
		return &push.BroadcastRoomReply{}, nil
	}

	idx := cityhash.CityHash32([]byte(req.RoomID), uint32(len(req.RoomID))) % s.roomWorkerNum
	ch := s.roomWorkers[idx]

	enqueueCtx := ctx
	if s.roomWorkerEnqueueTimeout > 0 {
		var cancel context.CancelFunc
		enqueueCtx, cancel = context.WithTimeout(ctx, s.roomWorkerEnqueueTimeout)
		defer cancel()
	}

	select {
	case ch <- req:
		return &push.BroadcastRoomReply{}, nil
	case <-enqueueCtx.Done():
		if errors.Is(enqueueCtx.Err(), context.DeadlineExceeded) {
			return nil, status.Error(codes.ResourceExhausted, "room broadcast queue overloaded")
		}
		return nil, enqueueCtx.Err()
	}
}

func (s *ConnectNodeServer) Rooms(ctx context.Context, req *push.RoomsReq) (*push.RoomsReply, error) {
	var (
		roomIds = make(map[string]bool)
	)
	for _, bucket := range s.Buckets() {
		for roomID := range bucket.Rooms() {
			roomIds[roomID] = true
		}
	}
	return &push.RoomsReply{Rooms: roomIds}, nil
}

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
	"sync/atomic"
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
	queueStateMu             sync.RWMutex
	queueState               uint32
	queueRejectLogNano       int64
	stopDrainTimeout         time.Duration
	leaveWorkerWG            sync.WaitGroup
	roomWorkerWG             sync.WaitGroup
	leaveAccepted            int64
	leaveStarted             int64
	leaveCompleted           int64
	roomAccepted             int64
	roomStarted              int64
	roomCompleted            int64
	shutdownSummaryMu        sync.RWMutex
	lastShutdownSummary      shutdownDrainSummary
	stopOnce                 sync.Once
}

const (
	queueStateRunning uint32 = iota
	queueStateStopping
	queueStateDraining
	queueStateStopped
)

var (
	codeErrQueueStopping   = errors.New("worker queue stopping")
	codeErrQueueClosed     = errors.New("worker queue closed")
	errNilLeaveResponse    = errors.New("nil response")
	ErrWorkerQueueStopping = &queueStateError{
		cause: codeErrQueueStopping,
		code:  codes.Unavailable,
		msg:   "worker queue stopping",
	}
	ErrWorkerQueueClosed = &queueStateError{
		cause: codeErrQueueClosed,
		code:  codes.Unavailable,
		msg:   "worker queue closed",
	}
)

type queueStateError struct {
	cause error
	code  codes.Code
	msg   string
}

func (e *queueStateError) Error() string {
	return e.msg
}

func (e *queueStateError) Unwrap() error {
	return e.cause
}

func (e *queueStateError) GRPCStatus() *status.Status {
	return status.New(e.code, e.msg)
}

type leaveTask struct {
	key        string
	userID     string
	roomID     string
	attempts   int
	enqueuedAt time.Time
}

type drainOutcome struct {
	Completed        int64
	TimeoutAbandoned int64
	NotStarted       int64
}

type shutdownDrainSummary struct {
	TimedOut bool
	Duration time.Duration
	Leave    drainOutcome
	Room     drainOutcome
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
		queueState:               queueStateRunning,
		stopDrainTimeout:         5 * time.Second,
		stopRoomSync:             make(chan struct{}),
	}

	sharedWriterCfg := cfg.SharedWriter
	if sharedWriterCfg == nil {
		sharedWriterCfg = &config.SharedWriterConfig{
			BatchSize:     32,
			MaxBatchBytes: 64 * 1024,
			FlushInterval: 500 * time.Millisecond,
			QueueSize:     1024,
		}
	}
	leaveQueueCfg := cfg.LeaveQueue
	if leaveQueueCfg == nil {
		leaveQueueCfg = &config.LeaveQueueConfig{
			RetryDelay:  200 * time.Millisecond,
			MaxAttempts: 3,
		}
	}
	server.leaveRetryDelay = leaveQueueCfg.RetryDelay
	server.leaveMaxAttempts = leaveQueueCfg.MaxAttempts

	server.sharedWriter = newSharedWriteManager(
		0,
		sharedWriterCfg.BatchSize,
		sharedWriterCfg.MaxBatchBytes,
		sharedWriterCfg.FlushInterval,
		sharedWriterCfg.QueueSize,
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
		stopBegin := time.Now()
		s.queueStateMu.Lock()
		if s.queueState != queueStateRunning {
			s.queueStateMu.Unlock()
			return
		}
		s.queueState = queueStateStopping
		leaveQueue := s.leaveQueue
		roomWorkers := append([]chan *push.BroadcastRoomReq(nil), s.roomWorkers...)
		sharedWriter := s.sharedWriter
		drainTimeout := s.stopDrainTimeout
		if drainTimeout <= 0 {
			drainTimeout = 5 * time.Second
		}
		s.queueState = queueStateDraining
		s.queueStateMu.Unlock()

		if leaveQueue != nil {
			close(leaveQueue)
		}
		for _, ch := range roomWorkers {
			if ch != nil {
				close(ch)
			}
		}

		waitDone := make(chan struct{})
		go func() {
			s.leaveWorkerWG.Wait()
			s.roomWorkerWG.Wait()
			close(waitDone)
		}()
		timedOut := false
		select {
		case <-waitDone:
		case <-time.After(drainTimeout):
			timedOut = true
		}
		if sharedWriter != nil {
			sharedWriter.Stop()
		}

		s.queueStateMu.Lock()
		s.leaveQueue = nil
		s.leaveWorkerNum = 0
		s.roomWorkers = nil
		s.roomWorkerNum = 0
		s.queueState = queueStateStopped
		s.queueStateMu.Unlock()

		summary := s.buildShutdownSummary(timedOut, time.Since(stopBegin))
		s.storeShutdownSummary(summary)
		s.recordShutdownSummary(summary)
		wsLog("ℹ️  [Shutdown] drain summary timeout=%t duration=%v leave(completed=%d timeout_abandoned=%d not_started=%d) room(completed=%d timeout_abandoned=%d not_started=%d)",
			summary.TimedOut,
			summary.Duration,
			summary.Leave.Completed,
			summary.Leave.TimeoutAbandoned,
			summary.Leave.NotStarted,
			summary.Room.Completed,
			summary.Room.TimeoutAbandoned,
			summary.Room.NotStarted,
		)
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
	s.queueState = queueStateRunning
	for i := uint64(0); i < workerNum; i++ {
		ch := make(chan *push.BroadcastRoomReq, queueSize)
		s.roomWorkers[i] = ch
		s.roomWorkerWG.Add(1)
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
	s.queueState = queueStateRunning
	for i := uint64(0); i < workerNum; i++ {
		s.leaveWorkerWG.Add(1)
		go s.leaveWorkerProc(s.leaveQueue)
	}
}

func (s *ConnectNodeServer) leaveWorkerProc(ch <-chan *leaveTask) {
	defer s.leaveWorkerWG.Done()
	for task := range ch {
		if task == nil {
			continue
		}
		atomic.AddInt64(&s.leaveStarted, 1)
		s.processLeaveTask(task)
		atomic.AddInt64(&s.leaveCompleted, 1)
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

func (s *ConnectNodeServer) EnqueueLeaveWithShutdownFallback(userID, roomID string) error {
	if s == nil || userID == "" || roomID == "" {
		return nil
	}
	err := s.EnqueueLeave(userID, roomID)
	if err == nil {
		return nil
	}
	if !isLeaveEnqueueCompensable(err) {
		return err
	}
	s.dispatchDirectLeaveCompensation(userID, roomID)
	return nil
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
			s.queueStateMu.RLock()
			state := s.queueState
			leaveQueue := s.leaveQueue
			s.queueStateMu.RUnlock()
			if state != queueStateRunning || leaveQueue == nil {
				err := queueStateErr(state)
				s.recordEnqueueFailure("leave_queue", err)
				return err
			}
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

	s.queueStateMu.RLock()
	state := s.queueState
	leaveQueue := s.leaveQueue
	if state != queueStateRunning || leaveQueue == nil {
		s.queueStateMu.RUnlock()
		if markPending {
			s.leavePendingMu.Lock()
			delete(s.leavePending, task.key)
			s.leavePendingMu.Unlock()
		}
		err := queueStateErr(state)
		s.recordEnqueueFailure("leave_queue", err)
		return err
	}
	select {
	case leaveQueue <- task:
		s.queueStateMu.RUnlock()
		atomic.AddInt64(&s.leaveAccepted, 1)
		return nil
	case <-enqueueCtx.Done():
		s.queueStateMu.RUnlock()
		if markPending {
			s.leavePendingMu.Lock()
			delete(s.leavePending, task.key)
			s.leavePendingMu.Unlock()
		}
		err := enqueueCtx.Err()
		s.recordEnqueueFailure("leave_queue", err)
		return err
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

func isLeaveEnqueueCompensable(err error) bool {
	return errors.Is(err, ErrWorkerQueueStopping) || errors.Is(err, ErrWorkerQueueClosed)
}

func (s *ConnectNodeServer) dispatchDirectLeaveCompensation(userID, roomID string) {
	if s == nil || userID == "" || roomID == "" {
		return
	}
	maxAttempts := s.leaveMaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	delay := s.leaveRetryDelay
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}
	go func() {
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if s.controllerClient == nil {
				wsLog("🚨 [LeaveCompensate] controller client unavailable user=%s room=%s", userID, roomID)
				if s.metrics != nil {
					s.metrics.RecordLeaveOutcome(context.Background(), "final_failure", "controller_unavailable")
				}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			start := time.Now()
			resp, err := s.controllerClient.LeaveRoom(ctx, &controller.LeaveRoomRequest{
				UserId: userID,
				RoomId: roomID,
			}, grpc.WaitForReady(true))
			cancel()
			elapsed := time.Since(start)
			if err == nil && resp != nil {
				wsLog("✅ [LeaveCompensate] LeaveRoom 成功 user=%s room=%s attempts=%d elapsed=%v", userID, roomID, attempt, elapsed)
				if s.metrics != nil {
					s.metrics.RecordLeaveOutcome(context.Background(), "success", "none")
				}
				return
			}
			if err == nil {
				err = errNilLeaveResponse
			}
			if attempt < maxAttempts {
				wsLog("⚠️  [LeaveCompensate] LeaveRoom 重试 user=%s room=%s attempts=%d elapsed=%v err=%v", userID, roomID, attempt, elapsed, err)
				time.Sleep(delay)
				continue
			}
			wsLog("🚨 [LeaveCompensate] LeaveRoom 最终失败 user=%s room=%s attempts=%d elapsed=%v err=%v", userID, roomID, attempt, elapsed, err)
			if s.metrics != nil {
				reason := "rpc_error"
				if errors.Is(err, errNilLeaveResponse) {
					reason = "nil_response"
				}
				s.metrics.RecordLeaveOutcome(context.Background(), "final_failure", reason)
			}
			return
		}
	}()
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
	defer s.roomWorkerWG.Done()
	for req := range ch {
		if req == nil || req.Proto == nil || req.RoomID == "" {
			continue
		}
		atomic.AddInt64(&s.roomStarted, 1)
		for _, bucket := range s.Buckets() {
			if room := bucket.Room(req.RoomID); room != nil {
				room.PushMsg(req.Proto)
			}
		}
		atomic.AddInt64(&s.roomCompleted, 1)
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
	s.queueStateMu.RLock()
	state := s.queueState
	roomWorkers := s.roomWorkers
	roomWorkerNum := s.roomWorkerNum
	if state != queueStateRunning {
		s.queueStateMu.RUnlock()
		err := queueStateErr(state)
		s.recordEnqueueFailure("broadcast_queue", err)
		return nil, err
	}
	if len(roomWorkers) == 0 || roomWorkerNum == 0 {
		s.queueStateMu.RUnlock()
		for _, bucket := range s.Buckets() {
			if room := bucket.Room(req.RoomID); room != nil {
				room.PushMsg(req.Proto)
			}
		}
		return &push.BroadcastRoomReply{}, nil
	}

	idx := cityhash.CityHash32([]byte(req.RoomID), uint32(len(req.RoomID))) % roomWorkerNum
	ch := roomWorkers[idx]

	enqueueCtx := ctx
	if s.roomWorkerEnqueueTimeout > 0 {
		var cancel context.CancelFunc
		enqueueCtx, cancel = context.WithTimeout(ctx, s.roomWorkerEnqueueTimeout)
		defer cancel()
	}

	select {
	case ch <- req:
		s.queueStateMu.RUnlock()
		atomic.AddInt64(&s.roomAccepted, 1)
		return &push.BroadcastRoomReply{}, nil
	case <-enqueueCtx.Done():
		s.queueStateMu.RUnlock()
		if errors.Is(enqueueCtx.Err(), context.DeadlineExceeded) {
			s.recordEnqueueFailure("broadcast_queue", enqueueCtx.Err())
			return nil, status.Error(codes.ResourceExhausted, "room broadcast queue overloaded")
		}
		s.recordEnqueueFailure("broadcast_queue", enqueueCtx.Err())
		return nil, enqueueCtx.Err()
	}
}

func queueStateErr(state uint32) error {
	if state == queueStateStopped {
		return ErrWorkerQueueClosed
	}
	return ErrWorkerQueueStopping
}

func enqueueRejectReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "queue_full"
	case errors.Is(err, ErrWorkerQueueStopping):
		return "stopping"
	case errors.Is(err, ErrWorkerQueueClosed):
		return "closed"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "unknown"
	}
}

var criticalEnqueueFailureRecorder = recordCriticalEnqueueFailure

func (s *ConnectNodeServer) recordEnqueueFailure(source string, err error) {
	if s == nil {
		return
	}
	reason := enqueueRejectReason(err)
	criticalEnqueueFailureRecorder(source, reason)
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&s.queueRejectLogNano)
	if now-last < int64(time.Second) {
		return
	}
	if !atomic.CompareAndSwapInt64(&s.queueRejectLogNano, last, now) {
		return
	}
	wsLog("⚠️  [Queue] enqueue rejected source=%s reason=%s err=%v", source, reason, err)
}

func classifyDrainOutcome(accepted, started, completed int64, timedOut bool) drainOutcome {
	if accepted < 0 {
		accepted = 0
	}
	if started < 0 {
		started = 0
	}
	if completed < 0 {
		completed = 0
	}
	if started > accepted {
		started = accepted
	}
	if completed > started {
		completed = started
	}
	out := drainOutcome{
		Completed: completed,
	}
	if timedOut {
		out.TimeoutAbandoned = started - completed
		if out.TimeoutAbandoned < 0 {
			out.TimeoutAbandoned = 0
		}
		out.NotStarted = accepted - started
		if out.NotStarted < 0 {
			out.NotStarted = 0
		}
	}
	return out
}

func (s *ConnectNodeServer) buildShutdownSummary(timedOut bool, duration time.Duration) shutdownDrainSummary {
	leaveAccepted := atomic.LoadInt64(&s.leaveAccepted)
	leaveStarted := atomic.LoadInt64(&s.leaveStarted)
	leaveCompleted := atomic.LoadInt64(&s.leaveCompleted)
	roomAccepted := atomic.LoadInt64(&s.roomAccepted)
	roomStarted := atomic.LoadInt64(&s.roomStarted)
	roomCompleted := atomic.LoadInt64(&s.roomCompleted)
	return shutdownDrainSummary{
		TimedOut: timedOut,
		Duration: duration,
		Leave:    classifyDrainOutcome(leaveAccepted, leaveStarted, leaveCompleted, timedOut),
		Room:     classifyDrainOutcome(roomAccepted, roomStarted, roomCompleted, timedOut),
	}
}

func (s *ConnectNodeServer) storeShutdownSummary(summary shutdownDrainSummary) {
	s.shutdownSummaryMu.Lock()
	s.lastShutdownSummary = summary
	s.shutdownSummaryMu.Unlock()
}

func (s *ConnectNodeServer) LastShutdownDrainSummary() shutdownDrainSummary {
	if s == nil {
		return shutdownDrainSummary{}
	}
	s.shutdownSummaryMu.RLock()
	defer s.shutdownSummaryMu.RUnlock()
	return s.lastShutdownSummary
}

func (s *ConnectNodeServer) recordShutdownSummary(summary shutdownDrainSummary) {
	recordCriticalShutdownDrain("leave_queue", "completed", summary.Leave.Completed)
	recordCriticalShutdownDrain("leave_queue", "timeout_abandoned", summary.Leave.TimeoutAbandoned)
	recordCriticalShutdownDrain("leave_queue", "not_started", summary.Leave.NotStarted)
	recordCriticalShutdownDrain("room_worker", "completed", summary.Room.Completed)
	recordCriticalShutdownDrain("room_worker", "timeout_abandoned", summary.Room.TimeoutAbandoned)
	recordCriticalShutdownDrain("room_worker", "not_started", summary.Room.NotStarted)
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

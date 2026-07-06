package main

import (
		"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	getty "github.com/AlexStocks/getty/transport"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

type mockGettySession struct {
	mu         sync.Mutex
	writes     [][][]byte
	pkgWrites  []any
	record     bool
	closed     atomic.Bool
	readTO     time.Duration
	writeTO    time.Duration
	attributes map[any]any
}

func (m *mockGettySession) Reset() {}
func (m *mockGettySession) Conn() net.Conn { return nil }
func (m *mockGettySession) Stat() string { return "mock-session" }
func (m *mockGettySession) IsClosed() bool { return m.closed.Load() }
func (m *mockGettySession) EndPoint() getty.EndPoint { return nil }
func (m *mockGettySession) SetMaxMsgLen(int) {}
func (m *mockGettySession) SetName(string) {}
func (m *mockGettySession) SetEventListener(getty.EventListener) {}
func (m *mockGettySession) SetPkgHandler(getty.ReadWriter) {}
func (m *mockGettySession) SetReader(getty.Reader) {}
func (m *mockGettySession) SetWriter(getty.Writer) {}
func (m *mockGettySession) SetCronPeriod(int) {}
func (m *mockGettySession) SetWaitTime(time.Duration) {}
func (m *mockGettySession) GetAttribute(key any) any {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attributes == nil {
		return nil
	}
	return m.attributes[key]
}
func (m *mockGettySession) SetAttribute(key, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attributes == nil {
		m.attributes = make(map[any]any)
	}
	m.attributes[key] = value
}
func (m *mockGettySession) RemoveAttribute(key any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.attributes, key)
}
func (m *mockGettySession) WritePkg(pkg any, _ time.Duration) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pkgWrites = append(m.pkgWrites, pkg)
	return 1, 1, nil
}
func (m *mockGettySession) WriteBytes(p []byte) (int, error) {
	return m.WriteBytesArray(p)
}
func (m *mockGettySession) WriteBytesArray(pkgs ...[]byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	if m.record {
		batch := make([][]byte, len(pkgs))
		for i, pkg := range pkgs {
			cp := append([]byte(nil), pkg...)
			batch[i] = cp
			total += len(cp)
		}
		m.writes = append(m.writes, batch)
		return total, nil
	}
	for _, pkg := range pkgs {
		total += len(pkg)
	}
	return total, nil
}
func (m *mockGettySession) Close() { m.closed.Store(true) }
func (m *mockGettySession) AddCloseCallback(any, any, getty.CallBackFunc) {}
func (m *mockGettySession) RemoveCloseCallback(any, any) {}
func (m *mockGettySession) ID() uint32 { return 1 }
func (m *mockGettySession) SetCompressType(getty.CompressType) {}
func (m *mockGettySession) LocalAddr() string { return "local" }
func (m *mockGettySession) RemoteAddr() string { return "remote" }
func (m *mockGettySession) IncReadPkgNum() {}
func (m *mockGettySession) IncWritePkgNum() {}
func (m *mockGettySession) UpdateActive() {}
func (m *mockGettySession) GetActive() time.Time { return time.Now() }
func (m *mockGettySession) ReadTimeout() time.Duration { return m.readTO }
func (m *mockGettySession) SetReadTimeout(d time.Duration) { m.readTO = d }
func (m *mockGettySession) WriteTimeout() time.Duration { return m.writeTO }
func (m *mockGettySession) SetWriteTimeout(d time.Duration) { m.writeTO = d }
func (m *mockGettySession) Send(any) (int, error) { return 0, nil }
func (m *mockGettySession) CloseConn(int) {}
func (m *mockGettySession) SetSession(getty.Session) {}

func (m *mockGettySession) writeBatches() [][][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][][]byte, len(m.writes))
	copy(out, m.writes)
	return out
}

func (m *mockGettySession) recordedPkgs() []any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]any, len(m.pkgWrites))
	copy(out, m.pkgWrites)
	return out
}

func newRegisteredFlushShardWithSession(batchSize, maxBatchBytes int, flushInterval time.Duration, session *mockGettySession) (*flushShard, uint64, *ProtoMessageHandler, *mockGettySession) {
	shard := newFlushShard(0, batchSize, maxBatchBytes, flushInterval, 16)
	sessionID := uint64(1)
	owner := &ProtoMessageHandler{}
	if session == nil {
		session = &mockGettySession{record: true}
	}
	shard.handleEvent(writeEvent{
		kind:      writeEventRegister,
		sessionID: sessionID,
		session:   session,
		handler:   gettypkg.NewProtoPackageHandler(),
		owner:     owner,
	})
	return shard, sessionID, owner, session
}

func newRegisteredFlushShard(batchSize, maxBatchBytes int, flushInterval time.Duration) (*flushShard, uint64, *ProtoMessageHandler, *mockGettySession) {
	return newRegisteredFlushShardWithSession(batchSize, maxBatchBytes, flushInterval, nil)
}

func testProto(seq int32, bodySize int) *proto.Proto {
	return &proto.Proto{
		Ver:    1,
		Op:     1,
		Seq:    seq,
		Roomid: "room",
		Userid: "user",
		Body:   make([]byte, bodySize),
	}
}

func TestFlushTriggerByCount(t *testing.T) {
	shard, sessionID, owner, session := newRegisteredFlushShard(2, writeBatchMaxBytes, time.Hour)

	shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, 8)})
	if got := len(session.writeBatches()); got != 0 {
		t.Fatalf("expected no flush before reaching batch size, got %d", got)
	}

	shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(2, 8)})

	if got := atomic.LoadUint64(&owner.batchFlushes); got != 1 {
		t.Fatalf("expected 1 flush, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByCount); got != 1 {
		t.Fatalf("expected count-trigger flushes=1, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByBytes); got != 0 {
		t.Fatalf("expected bytes-trigger flushes=0, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByTimeout); got != 0 {
		t.Fatalf("expected timeout-trigger flushes=0, got %d", got)
	}

	writes := session.writeBatches()
	if len(writes) != 1 || len(writes[0]) != 2 {
		t.Fatalf("expected one 2-pkg batch, got %#v", writes)
	}
}

func TestFlushTriggerByBytes(t *testing.T) {
	shard, sessionID, owner, session := newRegisteredFlushShard(8, 32, time.Hour)

	shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, 64)})

	if got := atomic.LoadUint64(&owner.batchFlushes); got != 1 {
		t.Fatalf("expected 1 flush, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByCount); got != 0 {
		t.Fatalf("expected count-trigger flushes=0, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByBytes); got != 1 {
		t.Fatalf("expected bytes-trigger flushes=1, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByTimeout); got != 0 {
		t.Fatalf("expected timeout-trigger flushes=0, got %d", got)
	}

	writes := session.writeBatches()
	if len(writes) != 1 || len(writes[0]) != 1 {
		t.Fatalf("expected one single-pkg byte-threshold batch, got %#v", writes)
	}
}

func TestFlushTriggerByTimeout(t *testing.T) {
	shard, sessionID, owner, session := newRegisteredFlushShard(8, writeBatchMaxBytes, 5*time.Millisecond)

	shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, 8)})
	time.Sleep(20 * time.Millisecond)
	shard.handleTimer()

	if got := atomic.LoadUint64(&owner.batchFlushes); got != 1 {
		t.Fatalf("expected 1 flush, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByCount); got != 0 {
		t.Fatalf("expected count-trigger flushes=0, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByBytes); got != 0 {
		t.Fatalf("expected bytes-trigger flushes=0, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByTimeout); got != 1 {
		t.Fatalf("expected timeout-trigger flushes=1, got %d", got)
	}

	writes := session.writeBatches()
	if len(writes) != 1 || len(writes[0]) != 1 {
		t.Fatalf("expected one single-pkg timeout batch, got %#v", writes)
	}
}

func TestUnregisterFlushDoesNotPolluteTriggerCounters(t *testing.T) {
	shard, sessionID, owner, session := newRegisteredFlushShard(8, writeBatchMaxBytes, time.Hour)

	shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, 8)})
	shard.handleEvent(writeEvent{kind: writeEventUnregister, sessionID: sessionID})

	if got := atomic.LoadUint64(&owner.batchFlushes); got != 1 {
		t.Fatalf("expected unregister to flush pending batch once, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByCount); got != 0 {
		t.Fatalf("expected count-trigger flushes=0, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByBytes); got != 0 {
		t.Fatalf("expected bytes-trigger flushes=0, got %d", got)
	}
	if got := atomic.LoadUint64(&owner.batchFlushByTimeout); got != 0 {
		t.Fatalf("expected timeout-trigger flushes=0, got %d", got)
	}

	writes := session.writeBatches()
	if len(writes) != 1 || len(writes[0]) != 1 {
		t.Fatalf("expected unregister to flush one pending pkg, got %#v", writes)
	}
}

func benchmarkFlushByCount(b *testing.B, bodySize int) {
	for i := 0; i < b.N; i++ {
		shard, sessionID, _, _ := newRegisteredFlushShard(4, writeBatchMaxBytes, time.Hour)
		for seq := int32(0); seq < 4; seq++ {
			shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(seq+1, bodySize)})
		}
	}
}

func benchmarkFlushByBytes(b *testing.B, bodySize int) {
	for i := 0; i < b.N; i++ {
		shard, sessionID, _, _ := newRegisteredFlushShard(8, 128, time.Hour)
		shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, bodySize)})
	}
}

func benchmarkFlushByTimeout(b *testing.B, bodySize int) {
	for i := 0; i < b.N; i++ {
		shard, sessionID, _, _ := newRegisteredFlushShard(8, writeBatchMaxBytes, time.Hour)
		shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, bodySize)})
		st := shard.sessions[sessionID]
		st.deadline = time.Now().Add(-time.Millisecond)
		shard.handleTimer()
	}
}

func BenchmarkSharedWriterFlushByCount(b *testing.B) {
	b.ReportAllocs()
	benchmarkFlushByCount(b, 16)
}

func BenchmarkSharedWriterFlushByCountParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			shard, sessionID, _, _ := newRegisteredFlushShard(4, writeBatchMaxBytes, time.Hour)
			for seq := int32(0); seq < 4; seq++ {
				shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(seq+1, 16)})
			}
		}
	})
}

func BenchmarkSharedWriterFlushByBytes(b *testing.B) {
	b.ReportAllocs()
	benchmarkFlushByBytes(b, 256)
}

func BenchmarkSharedWriterFlushByBytesParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			shard, sessionID, _, _ := newRegisteredFlushShard(8, 128, time.Hour)
			shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, 256)})
		}
	})
}

func BenchmarkSharedWriterFlushByTimeout(b *testing.B) {
	b.ReportAllocs()
	benchmarkFlushByTimeout(b, 16)
}

func BenchmarkSharedWriterFlushByTimeoutParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			shard, sessionID, _, _ := newRegisteredFlushShard(8, writeBatchMaxBytes, time.Hour)
			shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: testProto(1, 16)})
			st := shard.sessions[sessionID]
			st.deadline = time.Now().Add(-time.Millisecond)
			shard.handleTimer()
		}
	})
}

func BenchmarkSharedWriterSteadyStateFlushByCount(b *testing.B) {
	shard, sessionID, _, _ := newRegisteredFlushShardWithSession(4, writeBatchMaxBytes, time.Hour, &mockGettySession{})
	msgs := []*proto.Proto{
		testProto(1, 16),
		testProto(2, 16),
		testProto(3, 16),
		testProto(4, 16),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, msg := range msgs {
			shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: msg})
		}
	}
}

func BenchmarkSharedWriterSteadyStateFlushByBytes(b *testing.B) {
	shard, sessionID, _, _ := newRegisteredFlushShardWithSession(8, 128, time.Hour, &mockGettySession{})
	msg := testProto(1, 256)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: msg})
	}
}

func BenchmarkSharedWriterSteadyStateFlushByTimeout(b *testing.B) {
	shard, sessionID, _, _ := newRegisteredFlushShardWithSession(8, writeBatchMaxBytes, time.Hour, &mockGettySession{})
	msg := testProto(1, 16)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		shard.handleEvent(writeEvent{kind: writeEventEnqueue, sessionID: sessionID, msg: msg})
		st := shard.sessions[sessionID]
		st.deadline = time.Now().Add(-time.Millisecond)
		shard.handleTimer()
	}
}

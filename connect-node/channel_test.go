package main

import (
	"sync/atomic"
	"testing"
	"time"

	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

func resetChannelDropCounters() {
	atomic.StoreInt64(&globalPushDropCount, 0)
	atomic.StoreInt64(&globalSignalDropCount, 0)
}

func mustEnqueueClientRequest(t *testing.T, ch *Channel, p *proto.Proto) {
	t.Helper()

	slot, err := ch.ClientReqQueue.Set()
	if err != nil {
		t.Fatalf("enqueue request failed: %v", err)
	}
	*slot = &gettypkg.ProtoWithBuffer{Proto: p}
	ch.ClientReqQueue.SetAdv()
}

func TestSignalIsNonBlockingWhenSignalChannelFull(t *testing.T) {
	resetChannelDropCounters()

	ch := NewChannel(4, 1)
	ch.Signal()

	done := make(chan struct{})
	go func() {
		ch.Signal()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Signal blocked while signal channel was full")
	}

	if got := ch.GetSignalDropCount(); got != 1 {
		t.Fatalf("expected channel signal drop count 1, got %d", got)
	}
	if got := GetGlobalSignalDropCount(); got != 1 {
		t.Fatalf("expected global signal drop count 1, got %d", got)
	}
}

func TestSignalDropDoesNotLoseQueuedRequests(t *testing.T) {
	resetChannelDropCounters()

	ch := NewChannel(4, 1)
	first := &proto.Proto{Op: 101, Seq: 1}
	second := &proto.Proto{Op: 102, Seq: 2}

	mustEnqueueClientRequest(t, ch, first)
	ch.Signal()
	mustEnqueueClientRequest(t, ch, second)
	ch.Signal()

	if got := ch.GetSignalDropCount(); got != 1 {
		t.Fatalf("expected one merged signal drop, got %d", got)
	}

	if got := ch.Ready(); got != proto.ProtoReady {
		t.Fatalf("expected ProtoReady, got %#v", got)
	}

	pwb, err := ch.ClientReqQueue.Get()
	if err != nil {
		t.Fatalf("expected first queued request: %v", err)
	}
	if pwb.Proto != first {
		t.Fatalf("expected first request %#v, got %#v", first, pwb.Proto)
	}
	ch.ClientReqQueue.GetAdv()

	pwb, err = ch.ClientReqQueue.Get()
	if err != nil {
		t.Fatalf("expected second queued request: %v", err)
	}
	if pwb.Proto != second {
		t.Fatalf("expected second request %#v, got %#v", second, pwb.Proto)
	}
	ch.ClientReqQueue.GetAdv()

	if _, err := ch.ClientReqQueue.Get(); err == nil {
		t.Fatal("expected queue to be empty after draining requests")
	}
}

func TestSignalRemainsDeliverableWhenBroadcastOccupiesSignalChannel(t *testing.T) {
	resetChannelDropCounters()

	ch := NewChannel(4, 1)
	broadcast := &proto.Proto{Op: 201, Seq: 10}
	request := &proto.Proto{Op: 202, Seq: 11}

	ch.signal <- broadcast
	mustEnqueueClientRequest(t, ch, request)
	ch.Signal()

	if got := ch.GetSignalDropCount(); got != 0 {
		t.Fatalf("expected no signal drop while ready is tracked independently, got %d", got)
	}

	first := ch.Ready()
	second := ch.Ready()
	if first == second {
		t.Fatalf("expected broadcast and ready to both be delivered, got duplicate %#v", first)
	}
	if first != broadcast && first != proto.ProtoReady {
		t.Fatalf("unexpected first delivery %#v", first)
	}
	if second != broadcast && second != proto.ProtoReady {
		t.Fatalf("unexpected second delivery %#v", second)
	}
	if first != broadcast && second != broadcast {
		t.Fatalf("expected broadcast delivery, got %#v and %#v", first, second)
	}
	if first != proto.ProtoReady && second != proto.ProtoReady {
		t.Fatalf("expected ProtoReady delivery, got %#v and %#v", first, second)
	}

	pwb, err := ch.ClientReqQueue.Get()
	if err != nil {
		t.Fatalf("expected queued request after ready notification: %v", err)
	}
	if pwb.Proto != request {
		t.Fatalf("expected queued request %#v, got %#v", request, pwb.Proto)
	}
}

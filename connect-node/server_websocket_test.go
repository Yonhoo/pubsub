package main

import (
	"strings"
	"testing"

	proto "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

func TestWriteProtoRequiresSharedWriter(t *testing.T) {
	h := &ProtoMessageHandler{}

	err := h.writeProto(nil, &proto.Proto{Op: 1}, "response")
	if err == nil {
		t.Fatal("expected writeProto to fail without shared writer")
	}
	if !strings.Contains(err.Error(), "shared writer unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

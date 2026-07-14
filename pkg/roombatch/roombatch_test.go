package roombatch

import (
	"testing"

	protocol "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	input := []*protocol.Proto{
		{Ver: 1, Op: 1000, Roomid: "room-a", Body: []byte("first")},
		{Ver: 1, Op: 1000, Roomid: "room-a", Body: []byte("second")},
	}

	data, err := Pack(input)
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	got, err := Unpack(data)
	if err != nil {
		t.Fatalf("Unpack() error = %v", err)
	}
	if len(got) != len(input) {
		t.Fatalf("len(Unpack()) = %d, want %d", len(got), len(input))
	}
	for i := range input {
		if got[i].GetRoomid() != input[i].GetRoomid() {
			t.Fatalf("got[%d].Roomid = %q, want %q", i, got[i].GetRoomid(), input[i].GetRoomid())
		}
		if string(got[i].GetBody()) != string(input[i].GetBody()) {
			t.Fatalf("got[%d].Body = %q, want %q", i, got[i].GetBody(), input[i].GetBody())
		}
	}
}

func TestPackSkipsNilProto(t *testing.T) {
	data, err := Pack([]*protocol.Proto{nil, {Ver: 1, Roomid: "room-a", Body: []byte("ok")}})
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	got, err := Unpack(data)
	if err != nil {
		t.Fatalf("Unpack() error = %v", err)
	}
	if len(got) != 1 || string(got[0].GetBody()) != "ok" {
		t.Fatalf("Unpack() = %+v, want single ok proto", got)
	}
}

func TestUnpackRejectsTruncatedFrame(t *testing.T) {
	if _, err := Unpack([]byte{0, 0, 0, 10, 1, 2}); err == nil {
		t.Fatal("Unpack() error = nil, want truncated frame error")
	}
}

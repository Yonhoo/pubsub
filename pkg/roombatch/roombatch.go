package roombatch

import (
	"encoding/binary"
	"fmt"

	protocol "github.com/livekit/psrpc/examples/pubsub/protocol/protocol"
	gproto "google.golang.org/protobuf/proto"
)

func Pack(protos []*protocol.Proto) ([]byte, error) {
	if len(protos) == 0 {
		return nil, nil
	}
	size := 0
	encoded := make([][]byte, 0, len(protos))
	for _, p := range protos {
		if p == nil {
			continue
		}
		data, err := gproto.Marshal(p)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, data)
		size += 4 + len(data)
	}
	out := make([]byte, 0, size)
	var lenBuf [4]byte
	for _, data := range encoded {
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
		out = append(out, lenBuf[:]...)
		out = append(out, data...)
	}
	return out, nil
}

func Unpack(data []byte) ([]*protocol.Proto, error) {
	protos := make([]*protocol.Proto, 0)
	for len(data) > 0 {
		if len(data) < 4 {
			return nil, fmt.Errorf("truncated batch length: %d", len(data))
		}
		n := int(binary.BigEndian.Uint32(data[:4]))
		data = data[4:]
		if n <= 0 || n > len(data) {
			return nil, fmt.Errorf("invalid batch frame size: %d remaining=%d", n, len(data))
		}
		var p protocol.Proto
		if err := gproto.Unmarshal(data[:n], &p); err != nil {
			return nil, err
		}
		protos = append(protos, &p)
		data = data[n:]
	}
	return protos, nil
}

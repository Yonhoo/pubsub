package protocol

import (
	"errors"
)

const (
	// OpAuth auth connnect
	OpAuth = int32(7)

	OpProtoReady = int32(10)

	MaxBodySize = int32(1 << 12)

	// OpProtoFinish proto finish
	OpProtoFinish = int32(11)
)

var (
	// ProtoReady proto ready
	ProtoReady = &Proto{Op: OpProtoReady}
	// ProtoFinish proto finish
	ProtoFinish = &Proto{Op: OpProtoFinish}

	ErrProtoPackLen = errors.New("default server codec pack length error")
	// ErrProtoHeaderLen proto header len error
	ErrProtoHeaderLen = errors.New("default server codec header length error")
)

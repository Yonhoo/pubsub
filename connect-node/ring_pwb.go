package main

import (
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	gettypkg "github.com/livekit/psrpc/examples/pubsub/pkg/getty"
)

// RingPWB 存储 ProtoWithBuffer 指针的 Ring Buffer
// 用于客户端请求队列，支持零拷贝
type RingPWB struct {
	rp   uint64
	num  uint64
	mask uint64

	wp   uint64
	data []*gettypkg.ProtoWithBuffer
}

func NewRingPWB(num int) *RingPWB {
	r := new(RingPWB)
	r.init(uint64(num))
	return r
}

// Init init ring.
func (r *RingPWB) Init(num int) {
	r.init(uint64(num))
}

func (r *RingPWB) init(num uint64) {
	// 2^N
	if num&(num-1) != 0 {
		for num&(num-1) != 0 {
			num &= num - 1
		}
		num <<= 1
	}
	r.data = make([]*gettypkg.ProtoWithBuffer, num)
	r.num = num
	r.mask = r.num - 1
}

func (r *RingPWB) Get() (pwb *gettypkg.ProtoWithBuffer, err error) {
	if r.rp == r.wp {
		return nil, pkg.ErrRingEmpty
	}

	pwb = r.data[r.rp&r.mask]
	return
}

func (r *RingPWB) GetAdv() {
	r.rp++
}

func (r *RingPWB) SetAdv() {
	r.wp++
}

func (r *RingPWB) Set() (pwb **gettypkg.ProtoWithBuffer, err error) {
	if r.wp-r.rp >= r.num {
		return nil, pkg.ErrRingFull
	}
	pwb = &r.data[r.wp&r.mask]
	return
}

func (r *RingPWB) Reset() {
	r.rp = 0
	r.wp = 0
}

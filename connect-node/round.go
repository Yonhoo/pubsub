package main

import (
	"github.com/livekit/psrpc/examples/pubsub/pkg"
	"github.com/livekit/psrpc/examples/pubsub/pkg/config"
)

type RoundOptions struct {
	Timer        int
	TimerSize    int
	Reader       int
	ReadBuf      int
	ReadBufSize  int
	Writer       int
	WriteBuf     int
	WriteBufSize int
}

type Round struct {
	readers []pkg.Pool
	writers []pkg.Pool
	timers  []pkg.Timer
	options RoundOptions
}

func NewRound(c *config.Config) (r *Round) {

	var i int

	r = &Round{
		options: RoundOptions{
			Reader:       c.TCPConfig.Reader,
			ReadBuf:      c.TCPConfig.ReadBuf,
			ReadBufSize:  c.TCPConfig.ReadBufSize,
			Writer:       c.TCPConfig.Writer,
			WriteBuf:     c.TCPConfig.WriteBuf,
			WriteBufSize: c.TCPConfig.WriteBufSize,
			Timer:        c.Protocol.Timer,
			TimerSize:    c.Protocol.TimerSize,
		},
	}

	// reader
	r.readers = make([]pkg.Pool, r.options.Reader)
	for i = 0; i < r.options.Reader; i++ {
		r.readers[i].Init(r.options.ReadBuf, r.options.ReadBufSize)
	}
	// writer
	r.writers = make([]pkg.Pool, r.options.Writer)
	for i = 0; i < r.options.Writer; i++ {
		r.writers[i].Init(r.options.WriteBuf, r.options.WriteBufSize)
	}
	// timer
	r.timers = make([]pkg.Timer, r.options.Timer)
	for i = 0; i < r.options.Timer; i++ {
		r.timers[i].Init(r.options.TimerSize)
	}

	return

}

func (r *Round) Timer(rn int) *pkg.Timer {
	return &(r.timers[rn%r.options.Timer])
}

// Reader get a reader memory buffer.
func (r *Round) Reader(rn int) *pkg.Pool {
	return &(r.readers[rn%r.options.Reader])
}

// Writer get a writer memory buffer pool.
func (r *Round) Writer(rn int) *pkg.Pool {
	return &(r.writers[rn%r.options.Writer])
}

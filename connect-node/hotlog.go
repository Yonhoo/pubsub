package main

import (
	"log"
	"os"
	"sync/atomic"
	"time"
)

// CONNECT_NODE_HOT_LOG=1 enables verbose logs in broadcast hot paths.
var hotPathLogEnabled = os.Getenv("CONNECT_NODE_HOT_LOG") == "1"

func hotLogf(format string, v ...interface{}) {
	if hotPathLogEnabled {
		log.Printf(format, v...)
	}
}

func hotLogEvery(last *int64, interval time.Duration, format string, v ...interface{}) {
	if !hotPathLogEnabled {
		return
	}
	now := time.Now().UnixNano()
	prev := atomic.LoadInt64(last)
	if now-prev < interval.Nanoseconds() {
		return
	}
	if atomic.CompareAndSwapInt64(last, prev, now) {
		log.Printf(format, v...)
	}
}

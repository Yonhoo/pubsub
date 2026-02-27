package main

type broadcastPayload struct {
	MsgID  int64  `json:"msg_id"`
	SendTs int64  `json:"send_ts"`
	Data   string `json:"data"`
}

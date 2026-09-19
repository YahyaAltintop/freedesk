package session

import "encoding/json"

// peekType reads just the `t` of a control frame, so one channel can carry
// more than one kind of conversation without every parser having to know about
// the others. A few microseconds against a 60 Hz input stream, and it keeps the
// transfer and clipboard packages independent of each other.
func peekType(data []byte) string {
	var envelope struct {
		T string `json:"t"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return ""
	}
	return envelope.T
}

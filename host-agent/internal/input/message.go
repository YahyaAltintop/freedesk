package input

import "encoding/json"

// Message is one input event from the viewer (see docs/PROTOCOL.md).
type Message struct {
	T    string  `json:"t"`    // "m" | "md" | "mu" | "w" | "kd" | "ku" | "hello"
	X    float64 `json:"x"`    // normalised [0,1]
	Y    float64 `json:"y"`    // normalised [0,1]
	B    int     `json:"b"`    // mouse button: 0=left 1=right 2=middle
	DX   int     `json:"dx"`   // horizontal wheel delta
	DY   int     `json:"dy"`   // vertical wheel delta
	Code string  `json:"code"` // KeyboardEvent.code
}

// Parse decodes a JSON input message.
func Parse(data []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(data, &m)
	return m, err
}

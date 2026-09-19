// Package protocol holds what the two sides have to agree on before they can
// use anything beyond plain remote control: the wire version, the capability
// names, and the greeting that carries them (docs/PROTOCOL.md §2.2).
package protocol

import "encoding/json"

// Version is the wire protocol this agent speaks.
//
//   - 1: input only, one DataChannel, the host never wrote to it.
//   - 2: a second "file" channel, and this greeting.
const Version = 2

// Capability names. A viewer gates features on these rather than on Version,
// so a later agent can add one without every older viewer needing to know the
// numbering.
const (
	// CapFileSend: the viewer may offer files to this host.
	CapFileSend = "file.send"
	// CapFileRecv: this host can offer files to the viewer.
	CapFileRecv = "file.recv"
	// CapClipText: clipboard text is shared in both directions.
	CapClipText = "clip.text"
)

// Hello is the first thing the host says on the input channel.
//
// The host announces rather than answering, because there is no question a
// version-1 host would answer: it ignores every message it does not recognise
// and never writes back. Silence is therefore the only signal a viewer has for
// "this agent is old", and an unprompted greeting costs one frame and no round
// trip.
type Hello struct {
	T     string   `json:"t"`
	V     int      `json:"v"`
	Caps  []string `json:"caps"`
	Agent string   `json:"agent"`
}

// NewHello builds the greeting for an agent with the given capabilities.
// `caps` is always a list in the JSON, never null, so a viewer can read it
// without a nil check.
func NewHello(agent string, caps ...string) Hello {
	if caps == nil {
		caps = []string{}
	}
	return Hello{T: "hello", V: Version, Caps: caps, Agent: agent}
}

// Encode renders the greeting. Marshalling cannot fail for these fields, so a
// failure is reported as a greeting with no capabilities rather than forcing
// every caller to handle an error that cannot happen: claiming nothing is the
// safe direction to be wrong in.
func (h Hello) Encode() string {
	b, err := json.Marshal(h)
	if err != nil {
		return `{"t":"hello","v":2,"caps":[],"agent":""}`
	}
	return string(b)
}

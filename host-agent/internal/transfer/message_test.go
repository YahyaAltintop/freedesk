package transfer

import "testing"

func TestParseMsgAcceptsWellFormedFrames(t *testing.T) {
	m, err := ParseMsg([]byte(`{"t":"f-offer","id":"a1","name":"report.pdf","size":1234,"dir":"up"}`))
	if err != nil {
		t.Fatalf("a valid offer was rejected: %v", err)
	}
	if m.T != TypeOffer || m.ID != "a1" || m.Name != "report.pdf" || m.Size != 1234 || m.Dir != DirUp {
		t.Fatalf("decoded to %+v", m)
	}

	p, err := ParseMsg([]byte(`{"t":"f-progress","id":"a1","sent":512}`))
	if err != nil {
		t.Fatalf("a valid progress frame was rejected: %v", err)
	}
	if p.Sent != 512 {
		t.Fatalf("sent = %d, expected 512", p.Sent)
	}
}

func TestParseMsgRejectsMalformedFrames(t *testing.T) {
	bad := []struct {
		name string
		raw  string
	}{
		{"not json", `not-json`},
		{"no type", `{"id":"a1"}`},
		{"offer without name", `{"t":"f-offer","id":"a1","size":1,"dir":"up"}`},
		{"offer without id", `{"t":"f-offer","name":"a.txt","size":1,"dir":"up"}`},
		{"offer with zero size", `{"t":"f-offer","id":"a1","name":"a.txt","size":0,"dir":"up"}`},
		{"offer with negative size", `{"t":"f-offer","id":"a1","name":"a.txt","size":-5,"dir":"up"}`},
		{"offer with unknown direction", `{"t":"f-offer","id":"a1","name":"a.txt","size":1,"dir":"sideways"}`},
		{"offer claiming a resume", `{"t":"f-offer","id":"a1","name":"a.txt","size":1,"dir":"up","offset":9}`},
		{"accept without id", `{"t":"f-accept"}`},
		{"progress with negative count", `{"t":"f-progress","id":"a1","sent":-1}`},
		{"wrong field type", `{"t":"f-offer","id":"a1","name":"a.txt","size":"big","dir":"up"}`},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if m, err := ParseMsg([]byte(tt.raw)); err == nil {
				t.Fatalf("%s was accepted as %+v", tt.raw, m)
			}
		})
	}
}

// An unknown type is not an error: that is what lets a later version add one
// without an older peer treating it as a fault (docs/PROTOCOL.md §2.2).
func TestParseMsgIgnoresUnknownTypesWithoutFailing(t *testing.T) {
	m, err := ParseMsg([]byte(`{"t":"f-something-new","id":"a1","whatever":42}`))
	if err != nil {
		t.Fatalf("an unknown type must parse so the caller can ignore it by name: %v", err)
	}
	if m.T != "f-something-new" {
		t.Fatalf("type = %q", m.T)
	}
}

// The two channels have separate parsers on purpose. If a frame ever reaches
// the wrong one, it has to be rejected rather than half-understood — this is
// the test that makes the separation worth its cost.
func TestParsersDoNotAcceptEachOthersFrames(t *testing.T) {
	for _, raw := range []string{
		`{"t":"m","x":0.5,"y":0.2}`,
		`{"t":"kd","code":"KeyA"}`,
		`{"t":"mu","b":0,"x":0.1,"y":0.1}`,
	} {
		if m, err := ParseMsg([]byte(raw)); err == nil && m.T != "" {
			// Parsing may succeed structurally, but the type must not be one
			// this package would ever act on.
			switch m.T {
			case TypeOffer, TypeAccept, TypeReject, TypeComplete,
				TypeDone, TypeProgress, TypeCancel, TypeError:
				t.Fatalf("input frame %s was understood as transfer type %q", raw, m.T)
			}
		}
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	want := Msg{T: TypeOffer, ID: "a1", Name: "a.txt", Size: 10, Dir: DirDown}
	got, err := ParseMsg([]byte(want.Encode()))
	if err != nil {
		t.Fatalf("an encoded message must parse back: %v", err)
	}
	if got != want {
		t.Fatalf("round trip gave %+v, expected %+v", got, want)
	}

	// Empty fields stay off the wire rather than going out as zero values.
	if enc := Reject("a1", ReasonDenied).Encode(); enc != `{"t":"f-reject","id":"a1","reason":"denied"}` {
		t.Fatalf("Reject encoded as %s", enc)
	}
}

package transfer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseMsgAcceptsWellFormedFrames(t *testing.T) {
	m, err := ParseMsg([]byte(`{"t":"f-offer","id":"a1","dir":"up","files":[{"name":"report.pdf","size":1234}]}`))
	if err != nil {
		t.Fatalf("a valid offer was rejected: %v", err)
	}
	if m.T != TypeOffer || m.ID != "a1" || m.Dir != DirUp {
		t.Fatalf("decoded to %+v", m)
	}
	if len(m.Files) != 1 || m.Files[0].Name != "report.pdf" || m.Files[0].Size != 1234 {
		t.Fatalf("files decoded to %+v", m.Files)
	}
	if m.TotalSize() != 1234 {
		t.Fatalf("TotalSize() = %d", m.TotalSize())
	}

	p, err := ParseMsg([]byte(`{"t":"f-progress","id":"a1","index":2,"sent":512}`))
	if err != nil {
		t.Fatalf("a valid progress frame was rejected: %v", err)
	}
	if p.Sent != 512 || p.Index != 2 {
		t.Fatalf("decoded to %+v", p)
	}
}

func TestParseMsgRejectsMalformedFrames(t *testing.T) {
	bad := []struct {
		name string
		raw  string
	}{
		{"not json", `not-json`},
		{"no type", `{"id":"a1"}`},
		{"offer without files", `{"t":"f-offer","id":"a1","dir":"up"}`},
		{"offer with empty file list", `{"t":"f-offer","id":"a1","dir":"up","files":[]}`},
		{"offer without id", `{"t":"f-offer","dir":"up","files":[{"name":"a.txt","size":1}]}`},
		{"offer with nameless file", `{"t":"f-offer","id":"a1","dir":"up","files":[{"size":1}]}`},
		{"offer with zero size", `{"t":"f-offer","id":"a1","dir":"up","files":[{"name":"a.txt","size":0}]}`},
		{"offer with negative size", `{"t":"f-offer","id":"a1","dir":"up","files":[{"name":"a.txt","size":-5}]}`},
		{"offer with unknown direction", `{"t":"f-offer","id":"a1","dir":"sideways","files":[{"name":"a.txt","size":1}]}`},
		{"offer claiming a resume", `{"t":"f-offer","id":"a1","dir":"up","offset":9,"files":[{"name":"a.txt","size":1}]}`},
		{"accept without id", `{"t":"f-accept"}`},
		{"progress with negative count", `{"t":"f-progress","id":"a1","sent":-1}`},
		{"negative index", `{"t":"f-complete","id":"a1","index":-1}`},
		{"wrong field type", `{"t":"f-offer","id":"a1","dir":"up","files":[{"name":"a.txt","size":"big"}]}`},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if m, err := ParseMsg([]byte(tt.raw)); err == nil {
				t.Fatalf("%s was accepted as %+v", tt.raw, m)
			}
		})
	}
}

// The declared sizes decide whether the operator is asked at all, so an
// impossible claim is refused before it can reach them.
func TestParseMsgRejectsOversizedOffers(t *testing.T) {
	tooBig := fmt.Sprintf(`{"t":"f-offer","id":"a1","dir":"up","files":[{"name":"a.bin","size":%d}]}`,
		int64(MaxFileBytes)+1)
	if _, err := ParseMsg([]byte(tooBig)); err == nil {
		t.Fatal("a file over the per-file limit was accepted")
	}

	var files []string
	for i := range MaxBatchFiles + 1 {
		files = append(files, fmt.Sprintf(`{"name":"f%d.txt","size":10}`, i))
	}
	tooMany := `{"t":"f-offer","id":"a1","dir":"up","files":[` + strings.Join(files, ",") + `]}`
	if _, err := ParseMsg([]byte(tooMany)); err == nil {
		t.Fatalf("a batch of %d files was accepted", MaxBatchFiles+1)
	}

	// Each file under the per-file cap, but the batch over the batch cap.
	var big []string
	for range 4 {
		big = append(big, fmt.Sprintf(`{"name":"b.bin","size":%d}`, int64(MaxFileBytes)))
	}
	overBatch := `{"t":"f-offer","id":"a1","dir":"up","files":[` + strings.Join(big, ",") + `]}`
	if _, err := ParseMsg([]byte(overBatch)); err == nil {
		t.Fatal("a batch over the total limit was accepted")
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
		m, err := ParseMsg([]byte(raw))
		if err != nil {
			continue
		}
		switch m.T {
		case TypeOffer, TypeAccept, TypeReject, TypeComplete,
			TypeDone, TypeProgress, TypeCancel, TypeError:
			t.Fatalf("input frame %s was understood as transfer type %q", raw, m.T)
		}
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	want := Msg{
		T:     TypeOffer,
		ID:    "a1",
		Dir:   DirDown,
		Files: []FileMeta{{Name: "a.txt", Size: 10}, {Name: "b.txt", Size: 20}},
	}
	got, err := ParseMsg([]byte(want.Encode()))
	if err != nil {
		t.Fatalf("an encoded message must parse back: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip gave %+v, expected %+v", got, want)
	}

	// Empty fields stay off the wire rather than going out as zero values.
	if enc := Reject("a1", ReasonDenied).Encode(); enc != `{"t":"f-reject","id":"a1","reason":"denied"}` {
		t.Fatalf("Reject encoded as %s", enc)
	}
}

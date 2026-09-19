package transfer

import (
	"encoding/json"
	"errors"
)

// Control-frame types on the "file" channel. They are prefixed so they can
// never be confused with an input verb: the two channels have separate parsers,
// and a frame arriving on the wrong one must be recognisably foreign rather
// than accidentally meaningful.
const (
	TypeRequest  = "f-request"  // viewer: please offer me something to download
	TypeOffer    = "f-offer"    // sender: I have this file for you
	TypeAccept   = "f-accept"   // receiver: ready, start sending
	TypeReject   = "f-reject"   // receiver: no, and why
	TypeComplete = "f-complete" // sender: that was the last chunk
	TypeDone     = "f-done"     // receiver: written and safely on disk
	TypeProgress = "f-progress" // receiver: this much has landed
	TypeCancel   = "f-cancel"   // either: stop now
	TypeError    = "f-error"    // either: stopping because something broke
)

// Reasons a transfer ends without completing. The viewer turns these into the
// sentence it shows, so they describe the situation rather than the code path.
const (
	ReasonDenied   = "denied"    // the operator said no
	ReasonBusy     = "busy"      // another transfer is already being decided
	ReasonTooLarge = "too-large" // over the size limit, or longer than declared
	ReasonBadName  = "bad-name"  // the name could not be made safe to write
	ReasonNoSpace  = "no-space"  // not enough room on disk
	ReasonIO       = "io"        // the disk refused
	ReasonGone     = "gone"      // the session ended mid-transfer
	ReasonNoFSA    = "no-fsa"    // the browser cannot stream a file this big
)

// Directions, named from the viewer's point of view because that is where the
// person deciding what to do is sitting.
const (
	DirUp   = "up"   // viewer → host
	DirDown = "down" // host → viewer
)

// Limits. The declared size is only ever a claim: it decides whether to ask the
// operator at all, and the receiver counts the bytes it actually writes.
const (
	MaxFileBytes    = 512 << 20 // per file, viewer → host
	MaxBatchFiles   = 32        // a prompt listing more than this is unreadable
	MaxBatchBytes   = 1 << 30   // per drop
	MaxSessionFiles = 200
	MaxSessionBytes = 4 << 30

	// ChunkBytes is the payload of one binary frame. The ceiling is the
	// browser's advertised max-message-size (262144 in Chrome, and pion falls
	// back to 65535 when the attribute is missing); the practical limit is
	// pion's 64 KiB read buffer, which a 64 KiB chunk pushes just past, forcing
	// a reallocation on every message. 32 KiB clears both.
	ChunkBytes = 32 << 10
)

// ErrBadMessage is returned for a control frame that is not valid for its type.
// Unlike an unknown type — which is ignored, so later versions can add one —
// this is a frame that claims to be something it is not.
var ErrBadMessage = errors.New("malformed transfer message")

// FileMeta is one file in an offer: what it is called and how big it claims to
// be. The size is a claim the receiver checks against what actually arrives.
type FileMeta struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Msg is one control frame. Binary frames on the same channel are payload and
// never reach here.
//
// This is deliberately a separate type from input.Message rather than more
// fields on it. That struct is flat and decoded for every message, so a key
// reused with a different JSON type breaks remote control itself — a steep
// price for sharing a struct between two parsers that no longer even share a
// channel.
//
// An offer covers a whole batch rather than one file, because the operator is
// asked once per batch: a prompt per file trains people to click Yes without
// reading, which destroys the only real control in the system. The files of a
// batch are then sent in order — the channel is ordered and reliable, so the
// receiver knows which file it is on by counting the completions.
type Msg struct {
	T     string     `json:"t"`
	ID    string     `json:"id,omitempty"`    // batch id
	Files []FileMeta `json:"files,omitempty"` // f-offer
	Index int        `json:"index,omitempty"` // which file of the batch
	Size  int64      `json:"size,omitempty"`  // f-complete: bytes sent for this file
	Sent  int64      `json:"sent,omitempty"`  // f-progress: bytes written so far
	// Offset is reserved for resuming an interrupted transfer and is always 0
	// today. It is in the schema from the start so adding resume later does not
	// have to change the contract.
	Offset int64  `json:"offset,omitempty"`
	Dir    string `json:"dir,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// TotalSize is what the batch claims to add up to.
func (m Msg) TotalSize() int64 {
	var total int64
	for _, f := range m.Files {
		total += f.Size
	}
	return total
}

// ParseMsg decodes and validates one control frame.
func ParseMsg(data []byte) (Msg, error) {
	var m Msg
	if err := json.Unmarshal(data, &m); err != nil {
		return Msg{}, err
	}
	if err := m.validate(); err != nil {
		return Msg{}, err
	}
	return m, nil
}

// validate checks the fields each type cannot work without. An unknown type is
// allowed through with only an id check so the caller can ignore it by name;
// rejecting it here would make every future message type an error on an older
// peer.
func (m Msg) validate() error {
	switch m.T {
	case "":
		return ErrBadMessage
	case TypeOffer:
		if m.ID == "" || m.Offset != 0 {
			return ErrBadMessage
		}
		if m.Dir != DirUp && m.Dir != DirDown {
			return ErrBadMessage
		}
		if len(m.Files) == 0 || len(m.Files) > MaxBatchFiles {
			return ErrBadMessage
		}
		for _, f := range m.Files {
			// Names are checked properly by SafeName later; here it is only
			// that the fields are present and the size is sane at all.
			if f.Name == "" || f.Size <= 0 || f.Size > MaxFileBytes {
				return ErrBadMessage
			}
		}
		if m.TotalSize() > MaxBatchBytes {
			return ErrBadMessage
		}
	case TypeRequest, TypeAccept, TypeReject, TypeComplete, TypeDone, TypeProgress, TypeCancel, TypeError:
		if m.ID == "" {
			return ErrBadMessage
		}
		if m.Size < 0 || m.Sent < 0 || m.Index < 0 {
			return ErrBadMessage
		}
	}
	return nil
}

// Encode renders a control frame. Marshalling these cannot fail — every field
// is a string or an int — so the error is dropped rather than propagated into
// every call site.
func (m Msg) Encode() string {
	b, err := json.Marshal(m)
	if err != nil {
		return `{"t":"f-error","reason":"io"}`
	}
	return string(b)
}

// Reject builds the refusal for an offer.
func Reject(id, reason string) Msg { return Msg{T: TypeReject, ID: id, Reason: reason} }

// Fail builds the message that ends a transfer that had already started.
func Fail(id, reason string) Msg { return Msg{T: TypeError, ID: id, Reason: reason} }

package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSplitRecord(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a\n", []string{"a"}},
		{"a\nb\n", []string{"a", "b"}},
		{"a\r\n", []string{"a"}},
		{"x\x00y", []string{"xy"}},
		{"", nil},
		{"\n", nil},
		{"no newline", []string{"no newline"}},
	}
	for _, c := range cases {
		if got := splitRecord(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitRecord(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRingTrimsInChunksAndKeepsOrder(t *testing.T) {
	r := newLineRing()
	trims := 0
	for i := 1; i <= maxActivityLines+1; i++ {
		if r.push(fmt.Sprintf("line %d", i)) {
			trims++
		}
	}
	if trims != 1 {
		t.Fatalf("pushing one line past the cap trimmed %d times, want once", trims)
	}
	if got := len(r.lines); got != maxActivityLines+1-activityDrop {
		t.Fatalf("%d lines kept, want %d", got, maxActivityLines+1-activityDrop)
	}
	if r.lines[0] != fmt.Sprintf("line %d", activityDrop+1) {
		t.Errorf("first kept line is %q, want the one after the dropped chunk", r.lines[0])
	}
	if last := r.lines[len(r.lines)-1]; last != fmt.Sprintf("line %d", maxActivityLines+1) {
		t.Errorf("last kept line is %q", last)
	}
	text := r.text()
	if strings.HasSuffix(text, "\r\n") || !strings.Contains(text, "\r\n") {
		t.Errorf("text must join with CRLF and not end with one: %q…", text[:20])
	}
}

func TestRingSurvivesABatchLargerThanTheCap(t *testing.T) {
	r := newLineRing()
	batch := make([]string, 2*maxActivityLines)
	for i := range batch {
		batch[i] = fmt.Sprint(i)
	}
	if !r.push(batch...) {
		t.Fatal("a batch larger than the cap must trim")
	}
	if len(r.lines) != maxActivityLines {
		t.Fatalf("%d lines kept, want the cap %d", len(r.lines), maxActivityLines)
	}
	if r.lines[len(r.lines)-1] != fmt.Sprint(2*maxActivityLines-1) {
		t.Error("the newest line must be kept")
	}
}

// recorder is a Window that only remembers what was appended.
type recorder struct{ records []string }

func (r *recorder) ShowCode(string, string)   {}
func (r *recorder) SetStatus(string)          {}
func (r *recorder) Append(s string)           { r.records = append(r.records, s) }
func (r *recorder) ShowUpdate(string, string) {}
func (r *recorder) Done(int)                  {}
func (r *recorder) Loop() int                 { return 0 }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no console") }

func TestTeeFeedsWindowAndIgnoresEchoErrors(t *testing.T) {
	win := &recorder{}
	w := Tee(win, failingWriter{})
	n, err := w.Write([]byte("hello\n"))
	if err != nil || n != 6 {
		t.Fatalf("Write = %d, %v; the echo's failure must not surface", n, err)
	}
	if !reflect.DeepEqual(win.records, []string{"hello\n"}) {
		t.Errorf("window got %q", win.records)
	}

	quiet := Tee(win, nil)
	if _, err := quiet.Write([]byte("again\n")); err != nil {
		t.Fatalf("a nil echo must be fine: %v", err)
	}
	if len(win.records) != 2 {
		t.Errorf("window got %q", win.records)
	}
}

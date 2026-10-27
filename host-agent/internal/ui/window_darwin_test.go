//go:build darwin && cgo

package ui

import (
	"fmt"
	"strings"
	"testing"
)

// The window shows whatever text it was handed last, so every hand-over has
// to be the whole log: lines in order, one to a line, the oldest dropped at
// the cap exactly as on Windows.
func TestTheWholeLogIsHandedOver(t *testing.T) {
	var got []string
	w := newWindow(func(text string) { got = append(got, text) })

	w.Append("first\n")
	w.Append("second\r\nthird\n")
	w.Append("\n") // nothing to show: nothing handed over
	if len(got) != 2 {
		t.Fatalf("%d hand-overs for two records with lines, want 2", len(got))
	}
	if want := "first\nsecond\nthird"; got[1] != want {
		t.Fatalf("handed over %q, want %q", got[1], want)
	}

	for i := range maxActivityLines {
		w.Append(fmt.Sprintf("line %d\n", i))
	}
	last := got[len(got)-1]
	lines := strings.Split(last, "\n")
	if len(lines) > maxActivityLines {
		t.Fatalf("%d lines handed over, more than the cap %d", len(lines), maxActivityLines)
	}
	if lines[len(lines)-1] != fmt.Sprintf("line %d", maxActivityLines-1) {
		t.Errorf("the newest line handed over is %q", lines[len(lines)-1])
	}
	if strings.Contains(last, "first") {
		t.Error("the oldest lines were not dropped at the cap")
	}
}

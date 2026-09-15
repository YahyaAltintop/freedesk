package input

import "testing"

func TestParse(t *testing.T) {
	m, err := Parse([]byte(`{"t":"m","x":0.5,"y":0.25}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.T != "m" || m.X != 0.5 || m.Y != 0.25 {
		t.Fatalf("unexpected: %+v", m)
	}

	md, err := Parse([]byte(`{"t":"md","b":1,"x":0.1,"y":0.2}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if md.T != "md" || md.B != 1 {
		t.Fatalf("unexpected: %+v", md)
	}

	w, err := Parse([]byte(`{"t":"w","dx":0,"dy":-120}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if w.DY != -120 {
		t.Fatalf("unexpected: %+v", w)
	}

	k, err := Parse([]byte(`{"t":"kd","code":"KeyA"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if k.T != "kd" || k.Code != "KeyA" {
		t.Fatalf("unexpected: %+v", k)
	}

	if _, err := Parse([]byte(`not-json`)); err == nil {
		t.Fatal("invalid JSON should have produced an error")
	}
}

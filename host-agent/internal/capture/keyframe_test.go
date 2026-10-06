package capture

import (
	"testing"
	"time"
)

// TestKeyframeRequestCoalesces: several requests before one is taken count as
// one, and taking clears it.
func TestKeyframeRequestCoalesces(t *testing.T) {
	var k KeyframeRequest
	if k.take() {
		t.Fatal("nothing was requested yet")
	}
	k.Request()
	k.Request()
	if !k.take() {
		t.Fatal("a request must be reported")
	}
	if k.take() {
		t.Fatal("the request must have been cleared by the first take")
	}
}

// TestKeyframeDue covers the rate limit: a request is honoured only once
// keyframeMinInterval has passed, and one that comes too soon is kept pending
// rather than dropped.
func TestKeyframeDue(t *testing.T) {
	if keyframeDue(nil, time.Hour) {
		t.Fatal("a nil request is never due")
	}

	var k KeyframeRequest
	if keyframeDue(&k, time.Hour) {
		t.Fatal("no request pending, nothing is due")
	}

	k.Request()
	if keyframeDue(&k, keyframeMinInterval-time.Millisecond) {
		t.Fatal("within the interval the keyframe must wait")
	}
	// The request must still be pending after being refused for the interval.
	if !keyframeDue(&k, keyframeMinInterval) {
		t.Fatal("once the interval has passed the pending request is due")
	}
	if keyframeDue(&k, time.Hour) {
		t.Fatal("the request was consumed, so nothing is due now")
	}
}

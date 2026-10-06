package capture

import (
	"strings"
	"testing"
)

// TestCheckDuplicationDesc: the capture copies the duplicated desktop into a
// B8G8R8A8 staging texture and reads it as such, and does not turn a rotated
// image, so a duplication of any other shape must be refused (the GDI copy
// takes over) rather than read as garbage.
func TestCheckDuplicationDesc(t *testing.T) {
	ok := dxgiOutduplDesc{width: 1920, height: 1080, format: dxgiFormatB8G8R8A8Unorm, rotation: dxgiModeRotationIdentity}
	if err := checkDuplicationDesc(ok); err != nil {
		t.Fatalf("B8G8R8A8, identity rotation: %v", err)
	}
	ok.rotation = dxgiModeRotationUnspecified
	if err := checkDuplicationDesc(ok); err != nil {
		t.Fatalf("B8G8R8A8, unspecified rotation: %v", err)
	}

	rotated := ok
	rotated.rotation = 2 // DXGI_MODE_ROTATION_ROTATE90
	if err := checkDuplicationDesc(rotated); err == nil || err.Error() != errMsgRotatedOutputNotSupported {
		t.Fatalf("rotated: got %v", err)
	}

	hdr := ok
	hdr.format = 10 // DXGI_FORMAT_R16G16B16A16_FLOAT
	err := checkDuplicationDesc(hdr)
	if err == nil || !strings.Contains(err.Error(), errMsgFormatNotSupported) || !strings.Contains(err.Error(), "10") {
		t.Fatalf("R16G16B16A16_FLOAT: got %v, want a refusal naming the format", err)
	}
}

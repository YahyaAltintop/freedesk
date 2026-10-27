//go:build darwin && cgo

package macos

// #include "events_darwin.h"
import "C"

import "time"

// DoubleClickInterval is the longest gap between two clicks that still makes
// a double click: the operator's own setting.
func DoubleClickInterval() time.Duration {
	return time.Duration(float64(C.fd_double_click_interval()) * float64(time.Second))
}

//go:build darwin && cgo

#import <AppKit/AppKit.h>

#include "events_darwin.h"

double fd_double_click_interval(void) {
	@autoreleasepool {
		return [NSEvent doubleClickInterval];
	}
}

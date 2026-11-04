//go:build darwin && cgo

#include <CoreGraphics/CoreGraphics.h>
#include <dispatch/dispatch.h>

#include "cgevent_darwin.h"

/* The one source every event comes from. Its state is the HID system's, so
 * events read as the hardware's; and it suppresses nothing: by default the
 * person at the Mac could not move their own mouse for a quarter of a second
 * after every event posted from a source. */
static CGEventSourceRef fd_source_ref;
static dispatch_once_t fd_source_once;

static void fd_make_source(void *unused) {
	(void)unused;
	fd_source_ref = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	if (fd_source_ref) {
		CGEventSourceSetLocalEventsSuppressionInterval(fd_source_ref, 0);
	}
}

static CGEventSourceRef fd_source(void) {
	dispatch_once_f(&fd_source_once, NULL, fd_make_source);
	return fd_source_ref;
}

/* fd_post sends an event in where the hardware's events enter the system,
 * stating the modifiers held, and lets go of it. */
static void fd_post(CGEventRef event, uint64_t flags) {
	if (!event) {
		return;
	}
	CGEventSetFlags(event, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, event);
	CFRelease(event);
}

int fd_cg_access(int prompt) {
	if (CGPreflightPostEventAccess()) {
		return 1;
	}
	return prompt && CGRequestPostEventAccess() ? 1 : 0;
}

int fd_cg_screen(double *w, double *h) {
	CGRect bounds = CGDisplayBounds(CGMainDisplayID());
	*w = bounds.size.width;
	*h = bounds.size.height;
	return bounds.size.width > 0 && bounds.size.height > 0;
}

void fd_cg_mouse(uint32_t type, double x, double y, uint32_t button, int64_t clicks, uint64_t flags) {
	CGEventRef event = CGEventCreateMouseEvent(fd_source(), (CGEventType)type, CGPointMake(x, y), (CGMouseButton)button);
	if (event && clicks > 0) {
		CGEventSetIntegerValueField(event, kCGMouseEventClickState, clicks);
	}
	fd_post(event, flags);
}

void fd_cg_key(uint16_t keycode, int down, int autorepeat, uint64_t flags) {
	CGEventRef event = CGEventCreateKeyboardEvent(fd_source(), (CGKeyCode)keycode, down != 0);
	if (event) {
		if (autorepeat) {
			CGEventSetIntegerValueField(event, kCGKeyboardEventAutorepeat, 1);
		}
		/* What CoreGraphics read off the key itself stays: a keypad key, or
		 * one reached with Fn, as the hardware's events say so too (and a
		 * program telling the keypad's keys from the others goes by it). */
		flags |= CGEventGetFlags(event) & (kCGEventFlagMaskNumericPad | kCGEventFlagMaskSecondaryFn);
	}
	fd_post(event, flags);
}

void fd_cg_flags_changed(uint16_t keycode, uint64_t flags) {
	/* A modifier key is not a key down or up to macOS but a change of the
	 * modifiers held, naming the key that changed them. */
	CGEventRef event = CGEventCreateKeyboardEvent(fd_source(), (CGKeyCode)keycode, true);
	if (event) {
		CGEventSetType(event, kCGEventFlagsChanged);
	}
	fd_post(event, flags);
}

void fd_cg_scroll(int32_t vertical, int32_t horizontal, uint64_t flags) {
	fd_post(CGEventCreateScrollWheelEvent2(fd_source(), kCGScrollEventUnitPixel, 2, vertical, horizontal, 0), flags);
}

void fd_cg_cursor(double *x, double *y) {
	CGEventRef event = CGEventCreate(NULL);
	CGPoint p = event ? CGEventGetLocation(event) : CGPointZero;
	if (event) {
		CFRelease(event);
	}
	*x = p.x;
	*y = p.y;
}

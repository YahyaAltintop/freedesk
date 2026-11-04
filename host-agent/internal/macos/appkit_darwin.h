//go:build darwin && cgo

/* Small helpers the Objective-C files share. Objective-C: never included from
 * a cgo preamble, which is C. */
#ifndef FREEDESK_APPKIT_DARWIN_H
#define FREEDESK_APPKIT_DARWIN_H

#import <AppKit/AppKit.h>
#include <stdio.h>

/* fd_string is a C string as an NSString: never nil, so it can go anywhere a
 * string must (nil there would raise an exception nothing can catch). */
static inline NSString *fd_string(const char *s) {
	NSString *t = s ? [NSString stringWithUTF8String:s] : nil;
	return t ?: @"";
}

/* fd_copy_c copies an NSString into a C buffer of size bytes, cut short if
 * it has to be. */
static inline void fd_copy_c(char *dst, size_t size, NSString *s) {
	snprintf(dst, size, "%s", s.UTF8String ?: "");
}

/* fd_wake posts an empty event. A loop that has been told to stop, from
 * anything but an event of its own, sees it only when the next event comes;
 * this is that event. */
static inline void fd_wake(void) {
	NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
	                                   location:NSZeroPoint
	                              modifierFlags:0
	                                  timestamp:0
	                               windowNumber:0
	                                    context:nil
	                                    subtype:0
	                                      data1:0
	                                      data2:0];
	[NSApp postEvent:wake atStart:NO];
}

/* fd_snapshot draws a window's contents into a PNG file, over the window's
 * background: the views alone are transparent there, which in dark mode is
 * white text on nothing. For tests. */
static inline BOOL fd_snapshot(NSWindow *window, NSString *path) {
	NSView *view = window.contentView;
	if (!view) {
		return NO;
	}
	NSRect r = view.bounds;
	NSBitmapImageRep *views = [view bitmapImageRepForCachingDisplayInRect:r];
	NSBitmapImageRep *out = [view bitmapImageRepForCachingDisplayInRect:r];
	if (!views || !out) {
		return NO;
	}
	[view cacheDisplayInRect:r toBitmapImageRep:views];
	[NSGraphicsContext saveGraphicsState];
	NSGraphicsContext.currentContext = [NSGraphicsContext graphicsContextWithBitmapImageRep:out];
	[view.effectiveAppearance performAsCurrentDrawingAppearance:^{
		[NSColor.windowBackgroundColor setFill];
		NSRectFill(r);
	}];
	[views drawInRect:r fromRect:NSZeroRect operation:NSCompositingOperationSourceOver fraction:1 respectFlipped:YES hints:nil];
	[NSGraphicsContext restoreGraphicsState];
	return [[out representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:path atomically:YES];
}

#endif

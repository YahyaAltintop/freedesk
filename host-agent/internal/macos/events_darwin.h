//go:build darwin && cgo

/* What AppKit knows about input that CoreGraphics does not say. */
#ifndef FREEDESK_EVENTS_DARWIN_H
#define FREEDESK_EVENTS_DARWIN_H

/* The double-click speed set in System Settings, in seconds. */
double fd_double_click_interval(void);

#endif

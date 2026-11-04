//go:build darwin && cgo

/*
 * Posting input on macOS: CoreGraphics events, made from one event source and
 * posted where the hardware's own events enter the system, so the pointer
 * moves and every application sees them as the local mouse and keyboard's. Go
 * (inject_darwin.go) decides what to post; these only post it.
 */
#ifndef FREEDESK_CGEVENT_DARWIN_H
#define FREEDESK_CGEVENT_DARWIN_H

#include <stdint.h>

/* Whether this process may post input. With prompt, macOS asks the operator
 * if it has never asked. */
int fd_cg_access(int prompt);

/* The main display's size in points; 0 when there is none. */
int fd_cg_screen(double *w, double *h);

/* A mouse event of CGEventType type at (x, y) in global points; clicks is the
 * click count it carries (0 for none). */
void fd_cg_mouse(uint32_t type, double x, double y, uint32_t button, int64_t clicks, uint64_t flags);

/* A key going down or up; autorepeat marks a down that repeats a held key. */
void fd_cg_key(uint16_t keycode, int down, int autorepeat, uint64_t flags);

/* A modifier key changing state: flags is the whole new set. */
void fd_cg_flags_changed(uint16_t keycode, uint64_t flags);

/* A scroll in pixels: vertical positive up, horizontal positive left. */
void fd_cg_scroll(int32_t vertical, int32_t horizontal, uint64_t flags);

/* Where the pointer is, in global points. */
void fd_cg_cursor(double *x, double *y);

#endif

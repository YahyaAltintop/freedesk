//go:build darwin && cgo

/*
 * The macOS screen source: ScreenCaptureKit hands over a display as BGRA
 * pixel buffers, already scaled by the GPU to the size the encoder takes and
 * with the pointer drawn in. Its callbacks push frames on a queue of their
 * own; the capture pulls the newest one on each tick of its clock
 * (fd_sck_grab). A plain C interface over Objective-C (sck_darwin.m), which
 * screen_darwin.go wraps for Go.
 */
#ifndef FREEDESK_SCK_DARWIN_H
#define FREEDESK_SCK_DARWIN_H

#include <stdint.h>

typedef struct fd_sck fd_sck;

/* One picture: top-down BGRA, stride bytes from one row to the next. bits
 * stays valid and unchanged until the next fd_sck_grab or fd_sck_close. */
typedef struct {
	void *bits;
	int w, h, stride;
} fd_sck_frame;

enum {
	FD_SCK_UNCHANGED = 0,   /* the previous picture is still the screen */
	FD_SCK_CHANGED = 1,     /* a new picture */
	FD_SCK_UNAVAILABLE = 2, /* no picture now: none yet, or the screen shows nothing */
	FD_SCK_FAILED = 3,      /* the stream has ended; err says why */
};

/* Whether this process may record the screen. With prompt, macOS asks the
 * operator if it has never asked. */
int fd_sck_access(int prompt);

/* The main display and its size in pixels; 0 when there is none. */
int fd_sck_display(uint32_t *id, int *w, int *h);

/* Starts capturing display id at width x height pixels. NULL, with a message
 * in err, when it cannot. Blocks for a few seconds at most. */
fd_sck *fd_sck_open(uint32_t id, int width, int height, char *err, int errlen);

/* The newest picture as of now (see the FD_SCK_ values). */
int fd_sck_grab(fd_sck *s, fd_sck_frame *out, char *err, int errlen);

void fd_sck_close(fd_sck *s);

/* Whether the screen can be shown now: a session at the console, not locked,
 * and the main display awake. */
int fd_session_readable(void);

#endif

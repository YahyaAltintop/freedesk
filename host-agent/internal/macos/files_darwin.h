//go:build darwin && cgo

/*
 * Files the agent receives: marking one as come from elsewhere, and how much
 * room the volume it lands on has. Any thread.
 */
#ifndef FREEDESK_FILES_DARWIN_H
#define FREEDESK_FILES_DARWIN_H

#include <stdint.h>

/* Marks the file at path the way a browser marks a download. Returns 0, with
 * a message in err, when it cannot. */
int fd_quarantine(const char *path, char *err, int errlen);

/* For tests: the file's quarantine type and the program named as having put
 * it there; returns 0 when the file carries no mark. */
int fd_quarantine_read(const char *path, char *type, int typelen, char *agent, int agentlen);

/* The bytes a file the operator asked for may still take on the volume path
 * is on, counting what macOS frees on demand. Returns 0 when it cannot say. */
int fd_volume_capacity(const char *path, uint64_t *bytes);

#endif

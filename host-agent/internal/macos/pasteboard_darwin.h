//go:build darwin && cgo

/*
 * The pasteboard: the clipboard every application copies to and pastes from
 * (the general pasteboard), or a pasteboard of a name of one's own. A plain C
 * interface over NSPasteboard (pasteboard_darwin.m), which
 * pasteboard_darwin.go wraps for Go. Strings are UTF-8 with a length; those
 * handed back are malloc'd, for the caller to free.
 */
#ifndef FREEDESK_PASTEBOARD_DARWIN_H
#define FREEDESK_PASTEBOARD_DARWIN_H

#include <stddef.h>
#include <stdint.h>

/* A retained NSPasteboard. */
typedef void *fd_pb;

/* The pasteboard of that name, or the general one for NULL or "". */
fd_pb fd_pb_open(const char *name);
void fd_pb_close(fd_pb pb);
/* Deletes a named pasteboard from the system. */
void fd_pb_release_globally(const char *name);

/* Changes whenever anything puts something on the pasteboard. Reading it
 * reads no contents. */
int64_t fd_pb_change_count(fd_pb pb);

/* Whether the contents include a representation of type (a UTI). Reads no
 * contents. */
int fd_pb_has_type(fd_pb pb, const char *type);

enum {
	FD_PB_ALLOWED = 0,     /* read freely */
	FD_PB_NEVER_ASKED = 1, /* macOS asks at the first read, then lists the agent in System Settings */
	FD_PB_ASK = 2,         /* macOS asks at every read */
	FD_PB_DENIED = 3,      /* macOS refuses every read */
};

/* Whether reading the contents would make macOS ask the operator first. */
int fd_pb_access(fd_pb pb);

/* The plain text on the pasteboard, or NULL when there is none. */
char *fd_pb_read_text(fd_pb pb, size_t *len);
/* Replaces the contents with plain text. */
int fd_pb_write_text(fd_pb pb, const char *text, size_t len);
/* Replaces the contents with one item holding each type's string. */
int fd_pb_write_strings(fd_pb pb, const char **types, const char **values, int n);

/* The paths of the files on the pasteboard; their number goes in *n. */
char **fd_pb_read_files(fd_pb pb, int *n);
/* Replaces the contents with files, for pasting in the Finder. */
int fd_pb_write_files(fd_pb pb, const char **paths, int n);
void fd_pb_free_strings(char **v, int n);

#endif

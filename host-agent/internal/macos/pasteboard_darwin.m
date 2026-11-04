//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

#include "pasteboard_darwin.h"

/* Whether macOS asks before the agent reads the clipboard came with the
 * macOS 15.4 SDK; one older than that cannot build the agent's policy. */
#ifndef __MAC_15_4
#error "building the agent needs the macOS 15.4 SDK or newer: Xcode 16.3 or newer, or its command line tools"
#endif

static NSPasteboard *fd_pb_ns(fd_pb pb) {
	return (__bridge NSPasteboard *)pb;
}

/* fd_pb_copy hands a string back to Go: malloc'd UTF-8, its length in len. */
static char *fd_pb_copy(NSString *s, size_t *len) {
	/* A string with a lone surrogate in it, which some programs do put on
	 * the pasteboard, has no UTF-8 as such: lossy, the bad unit comes out as
	 * U+FFFD rather than the whole string as nothing. */
	NSData *utf8 = [s dataUsingEncoding:NSUTF8StringEncoding allowLossyConversion:YES];
	if (!utf8) {
		return NULL;
	}
	char *out = malloc(utf8.length + 1);
	if (!out) {
		return NULL;
	}
	memcpy(out, utf8.bytes, utf8.length);
	out[utf8.length] = 0;
	if (len) {
		*len = utf8.length;
	}
	return out;
}

static NSString *fd_pb_string(const char *s);

fd_pb fd_pb_open(const char *name) {
	@autoreleasepool {
		NSString *n = fd_pb_string(name);
		NSPasteboard *pb = n.length > 0 ? [NSPasteboard pasteboardWithName:n] : [NSPasteboard generalPasteboard];
		return (fd_pb)CFBridgingRetain(pb);
	}
}

void fd_pb_close(fd_pb pb) {
	if (pb) {
		CFBridgingRelease(pb);
	}
}

void fd_pb_release_globally(const char *name) {
	@autoreleasepool {
		NSString *n = fd_pb_string(name);
		if (n.length > 0) {
			[[NSPasteboard pasteboardWithName:n] releaseGlobally];
		}
	}
}

int64_t fd_pb_change_count(fd_pb pb) {
	@autoreleasepool {
		return (int64_t)fd_pb_ns(pb).changeCount;
	}
}

int fd_pb_has_type(fd_pb pb, const char *type) {
	@autoreleasepool {
		NSString *t = fd_pb_string(type);
		return t && [fd_pb_ns(pb).types containsObject:t] ? 1 : 0;
	}
}

int fd_pb_access(fd_pb pb) {
	@autoreleasepool {
		if (@available(macOS 15.4, *)) {
			switch (fd_pb_ns(pb).accessBehavior) {
			case NSPasteboardAccessBehaviorAlwaysAllow:
				return FD_PB_ALLOWED;
			case NSPasteboardAccessBehaviorDefault:
				return FD_PB_NEVER_ASKED;
			case NSPasteboardAccessBehaviorAlwaysDeny:
				return FD_PB_DENIED;
			default:
				return FD_PB_ASK;
			}
		}
		return FD_PB_ALLOWED; /* before 15.4 macOS never asks */
	}
}

char *fd_pb_read_text(fd_pb pb, size_t *len) {
	@autoreleasepool {
		NSString *s = [fd_pb_ns(pb) stringForType:NSPasteboardTypeString];
		return s ? fd_pb_copy(s, len) : NULL;
	}
}

int fd_pb_write_text(fd_pb pb, const char *text, size_t len) {
	@autoreleasepool {
		NSString *s = [[NSString alloc] initWithBytes:text length:len encoding:NSUTF8StringEncoding];
		if (!s) {
			return 0;
		}
		NSPasteboard *p = fd_pb_ns(pb);
		[p clearContents];
		return [p setString:s forType:NSPasteboardTypeString] ? 1 : 0;
	}
}

/* fd_pb_string is a C string as an NSString; nil where it is not UTF-8,
 * which every caller checks: handing nil on would raise an Objective-C
 * exception, and nothing here can catch one. */
static NSString *fd_pb_string(const char *s) {
	return s ? [NSString stringWithUTF8String:s] : nil;
}

int fd_pb_write_strings(fd_pb pb, const char **types, const char **values, int n) {
	@autoreleasepool {
		NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
		for (int i = 0; i < n; i++) {
			NSString *type = fd_pb_string(types[i]), *value = fd_pb_string(values[i]);
			if (!type || !value) {
				return 0;
			}
			[item setString:value forType:type];
		}
		NSPasteboard *p = fd_pb_ns(pb);
		[p clearContents];
		return [p writeObjects:@[ item ]] ? 1 : 0;
	}
}

char **fd_pb_read_files(fd_pb pb, int *n) {
	@autoreleasepool {
		*n = 0;
		NSArray<NSURL *> *urls = [fd_pb_ns(pb) readObjectsForClasses:@[ NSURL.class ]
		                                                       options:@{NSPasteboardURLReadingFileURLsOnlyKey : @YES}];
		if (urls.count == 0) {
			return NULL;
		}
		char **out = calloc(urls.count, sizeof *out);
		if (!out) {
			return NULL;
		}
		int count = 0;
		for (NSURL *url in urls) {
			/* The Finder copies file reference URLs (/.file/id=...);
			 * filePathURL turns one into the file's path. That path comes in
			 * the file system's decomposed form ("u" and a combining
			 * diaeresis for "ü"); it is handed on composed, as names are
			 * typed and shown everywhere else. APFS finds a file under
			 * either form. */
			NSString *path = url.filePathURL.path.precomposedStringWithCanonicalMapping;
			if (path.length > 0) {
				char *p = fd_pb_copy(path, NULL);
				if (p) {
					out[count++] = p;
				}
			}
		}
		*n = count;
		return out;
	}
}

int fd_pb_write_files(fd_pb pb, const char **paths, int n) {
	@autoreleasepool {
		NSMutableArray<NSURL *> *urls = [NSMutableArray arrayWithCapacity:(NSUInteger)n];
		for (int i = 0; i < n; i++) {
			NSString *path = fd_pb_string(paths[i]);
			if (!path) {
				return 0;
			}
			[urls addObject:[NSURL fileURLWithPath:path]];
		}
		NSPasteboard *p = fd_pb_ns(pb);
		[p clearContents];
		return [p writeObjects:urls] ? 1 : 0;
	}
}

void fd_pb_free_strings(char **v, int n) {
	if (!v) {
		return;
	}
	for (int i = 0; i < n; i++) {
		free(v[i]);
	}
	free(v);
}

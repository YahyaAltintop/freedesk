//go:build darwin && cgo

#import <CoreGraphics/CoreGraphics.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#include <os/lock.h>
#include <stdio.h>

#include "sck_darwin.h"

/* How long a call into ScreenCaptureKit may take before the capture gives up
 * on it, how long a new stream gets to show its first picture, and how long a
 * stream gets to stop. */
static const int64_t fd_call_timeout = 5 * NSEC_PER_SEC;
static const int64_t fd_first_frame_timeout = NSEC_PER_SEC;
static const int64_t fd_stop_timeout = 2 * NSEC_PER_SEC;

static void fd_err(char *err, int errlen, NSString *msg) {
	snprintf(err, (size_t)errlen, "%s", msg.UTF8String ? msg.UTF8String : "unknown error");
}

static NSString *fd_describe(NSError *e) {
	if (!e) {
		return @"no reason given";
	}
	if ([e.domain isEqualToString:SCStreamErrorDomain] && e.code == SCStreamErrorUserDeclined) {
		return @"macOS does not let FreeDesk record the screen (System Settings, Privacy & Security, Screen & System Audio Recording)";
	}
	return [NSString stringWithFormat:@"%@ (%@ %ld)", e.localizedDescription, e.domain, (long)e.code];
}

/* FDOutput receives the stream's frames on the capture's queue and keeps the
 * newest one until the capture goroutine takes it. Nothing here calls into Go. */
@interface FDOutput : NSObject <SCStreamOutput, SCStreamDelegate> {
@public
	os_unfair_lock lock;
	CMSampleBufferRef pending;  /* the newest picture not yet grabbed, retained */
	BOOL blank;                 /* the screen shows nothing now (asleep, say) */
	BOOL stopped;               /* the stream has ended */
	NSString *reason;           /* why it ended */
	dispatch_semaphore_t first; /* signalled at the stream's first picture */
	BOOL seenFirst;
}
@end

@implementation FDOutput

- (instancetype)init {
	if ((self = [super init])) {
		lock = OS_UNFAIR_LOCK_INIT;
		first = dispatch_semaphore_create(0);
	}
	return self;
}

- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)sample ofType:(SCStreamOutputType)type {
	if (type != SCStreamOutputTypeScreen || !CMSampleBufferIsValid(sample)) {
		return;
	}
	CFArrayRef attachments = CMSampleBufferGetSampleAttachmentsArray(sample, false);
	if (!attachments || CFArrayGetCount(attachments) == 0) {
		return;
	}
	NSDictionary *info = (__bridge NSDictionary *)CFArrayGetValueAtIndex(attachments, 0);
	NSNumber *status = info[SCStreamFrameInfoStatus];
	if (!status) {
		return;
	}
	os_unfair_lock_lock(&lock);
	switch ((SCFrameStatus)status.integerValue) {
	case SCFrameStatusComplete:
	case SCFrameStatusStarted:
		if (CMSampleBufferGetImageBuffer(sample)) {
			/* An older picture nobody took is simply replaced: only the
			 * newest one is ever wanted. */
			if (pending) {
				CFRelease(pending);
			}
			pending = (CMSampleBufferRef)CFRetain(sample);
			blank = NO;
			if (!seenFirst) {
				seenFirst = YES;
				dispatch_semaphore_signal(first);
			}
		}
		break;
	case SCFrameStatusBlank:
	case SCFrameStatusSuspended:
		blank = YES;
		break;
	case SCFrameStatusStopped:
		stopped = YES;
		if (!reason) {
			reason = @"macOS stopped it";
		}
		break;
	default: /* SCFrameStatusIdle: nothing on the screen changed */
		break;
	}
	os_unfair_lock_unlock(&lock);
}

- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
	os_unfair_lock_lock(&lock);
	stopped = YES;
	reason = fd_describe(error);
	os_unfair_lock_unlock(&lock);
}

@end

/* FDCapture is what an fd_sck handle stands for. Everything but the output's
 * locked fields belongs to the capture goroutine's thread. */
@interface FDCapture : NSObject {
@public
	SCStream *stream;
	FDOutput *output;
	dispatch_queue_t queue;
	CMSampleBufferRef current; /* the picture the last grab handed out, retained */
	CVPixelBufferRef locked;    /* its pixels, locked for reading */
}
@end

@implementation FDCapture
@end

int fd_sck_access(int prompt) {
	if (CGPreflightScreenCaptureAccess()) {
		return 1;
	}
	return prompt && CGRequestScreenCaptureAccess() ? 1 : 0;
}

int fd_sck_display(uint32_t *id, int *w, int *h) {
	CGDirectDisplayID display = CGMainDisplayID();
	CGDisplayModeRef mode = CGDisplayCopyDisplayMode(display);
	if (!mode) {
		return 0;
	}
	*id = display;
	*w = (int)CGDisplayModeGetPixelWidth(mode);
	*h = (int)CGDisplayModeGetPixelHeight(mode);
	CGDisplayModeRelease(mode);
	return *w > 0 && *h > 0;
}

static void fd_release_current(FDCapture *c) {
	if (c->locked) {
		CVPixelBufferUnlockBaseAddress(c->locked, kCVPixelBufferLock_ReadOnly);
		c->locked = NULL;
	}
	if (c->current) {
		CFRelease(c->current);
		c->current = NULL;
	}
}

/* fd_stop ends the stream and lets go of every picture it handed over. */
static void fd_stop(FDCapture *c) {
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	[c->stream stopCaptureWithCompletionHandler:^(NSError *e) {
		dispatch_semaphore_signal(done);
	}];
	dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, fd_stop_timeout));
	[c->stream removeStreamOutput:c->output type:SCStreamOutputTypeScreen error:nil];
	/* A frame being handed over right now finishes before its buffers go. */
	dispatch_sync(c->queue, ^{
	});
	fd_release_current(c);
	FDOutput *o = c->output;
	os_unfair_lock_lock(&o->lock);
	if (o->pending) {
		CFRelease(o->pending);
		o->pending = NULL;
	}
	os_unfair_lock_unlock(&o->lock);
}

fd_sck *fd_sck_open(uint32_t id, int width, int height, char *err, int errlen) {
	@autoreleasepool {
		/* The displays ScreenCaptureKit can capture; the caller is never the
		 * main thread, so waiting here blocks nothing the answer needs. */
		dispatch_semaphore_t answered = dispatch_semaphore_create(0);
		__block SCShareableContent *content = nil;
		__block NSError *failure = nil;
		[SCShareableContent getShareableContentExcludingDesktopWindows:NO
		                                           onScreenWindowsOnly:YES
		                                             completionHandler:^(SCShareableContent *c, NSError *e) {
			content = c;
			failure = e;
			dispatch_semaphore_signal(answered);
		}];
		if (dispatch_semaphore_wait(answered, dispatch_time(DISPATCH_TIME_NOW, fd_call_timeout)) != 0) {
			fd_err(err, errlen, @"ScreenCaptureKit did not say which displays it can capture");
			return NULL;
		}
		if (!content) {
			fd_err(err, errlen, fd_describe(failure));
			return NULL;
		}
		SCDisplay *display = nil;
		for (SCDisplay *d in content.displays) {
			if (d.displayID == id) {
				display = d;
				break;
			}
		}
		if (!display) {
			fd_err(err, errlen, @"the main display is not one ScreenCaptureKit can capture");
			return NULL;
		}

		/* The whole display, every window on it. sRGB is the colour space
		 * the converter's BT.601 matrix and every viewer assume; the pointer
		 * is drawn in, as on Windows. */
		SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingWindows:@[]];
		SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
		config.width = (size_t)width;
		config.height = (size_t)height;
		config.pixelFormat = kCVPixelFormatType_32BGRA;
		config.colorSpaceName = kCGColorSpaceSRGB;
		config.showsCursor = YES;
		config.minimumFrameInterval = CMTimeMake(1, 30);
		/* The capture holds two buffers at most (the newest and the one being
		 * read); five leaves the stream room to keep going meanwhile. */
		config.queueDepth = 5;

		FDCapture *c = [[FDCapture alloc] init];
		c->output = [[FDOutput alloc] init];
		c->queue = dispatch_queue_create("app.freedesk.capture", DISPATCH_QUEUE_SERIAL);
		c->stream = [[SCStream alloc] initWithFilter:filter configuration:config delegate:c->output];
		NSError *added = nil;
		if (![c->stream addStreamOutput:c->output type:SCStreamOutputTypeScreen sampleHandlerQueue:c->queue error:&added]) {
			fd_err(err, errlen, fd_describe(added));
			return NULL;
		}
		dispatch_semaphore_t started = dispatch_semaphore_create(0);
		__block NSError *startFailure = nil;
		[c->stream startCaptureWithCompletionHandler:^(NSError *e) {
			startFailure = e;
			dispatch_semaphore_signal(started);
		}];
		if (dispatch_semaphore_wait(started, dispatch_time(DISPATCH_TIME_NOW, fd_call_timeout)) != 0) {
			fd_err(err, errlen, @"ScreenCaptureKit did not start the capture");
			fd_stop(c);
			return NULL;
		}
		if (startFailure) {
			fd_err(err, errlen, fd_describe(startFailure));
			return NULL;
		}
		/* The pipeline reads the picture's size at its first grab; a still
		 * screen sends its first picture at once, and one that does not
		 * within the timeout (asleep, say) starts out unavailable. */
		dispatch_semaphore_wait(c->output->first, dispatch_time(DISPATCH_TIME_NOW, fd_first_frame_timeout));
		return (fd_sck *)CFBridgingRetain(c);
	}
}

static void fd_fill(FDCapture *c, fd_sck_frame *out) {
	out->bits = CVPixelBufferGetBaseAddress(c->locked);
	out->w = (int)CVPixelBufferGetWidth(c->locked);
	out->h = (int)CVPixelBufferGetHeight(c->locked);
	out->stride = (int)CVPixelBufferGetBytesPerRow(c->locked);
}

int fd_sck_grab(fd_sck *s, fd_sck_frame *out, char *err, int errlen) {
	@autoreleasepool {
		FDCapture *c = (__bridge FDCapture *)(void *)s;
		FDOutput *o = c->output;
		os_unfair_lock_lock(&o->lock);
		CMSampleBufferRef next = o->pending;
		o->pending = NULL;
		BOOL stopped = o->stopped, blank = o->blank;
		NSString *reason = o->reason;
		os_unfair_lock_unlock(&o->lock);

		if (stopped) {
			if (next) {
				CFRelease(next);
			}
			fd_err(err, errlen, [@"the screen capture stopped: " stringByAppendingString:reason ?: @"no reason given"]);
			return FD_SCK_FAILED;
		}
		if (next) {
			/* The picture is read where the GPU left it, no copy: locked
			 * until the next grab, by which time the pipeline has converted
			 * it. */
			CVPixelBufferRef pixels = CMSampleBufferGetImageBuffer(next);
			if (pixels && CVPixelBufferLockBaseAddress(pixels, kCVPixelBufferLock_ReadOnly) == kCVReturnSuccess) {
				if (CVPixelBufferGetBaseAddress(pixels)) {
					fd_release_current(c);
					c->current = next;
					c->locked = pixels;
					fd_fill(c, out);
					return FD_SCK_CHANGED;
				}
				CVPixelBufferUnlockBaseAddress(pixels, kCVPixelBufferLock_ReadOnly);
			}
			CFRelease(next); /* a picture the processor cannot read: skipped */
		}
		if (blank || !c->current) {
			return FD_SCK_UNAVAILABLE;
		}
		fd_fill(c, out);
		return FD_SCK_UNCHANGED;
	}
}

void fd_sck_close(fd_sck *s) {
	if (!s) {
		return;
	}
	@autoreleasepool {
		FDCapture *c = CFBridgingRelease(s);
		fd_stop(c);
	}
}

int fd_session_readable(void) {
	@autoreleasepool {
		CFDictionaryRef raw = CGSessionCopyCurrentDictionary();
		if (!raw) {
			return 0; /* no window server session: nothing to capture */
		}
		NSDictionary *session = CFBridgingRelease(raw);
		/* Another user's session is at the screen (fast user switching). */
		NSNumber *console = session[(__bridge NSString *)kCGSessionOnConsoleKey];
		if (console && !console.boolValue) {
			return 0;
		}
		/* The lock screen. The key is not in a public header, but it is the
		 * one macOS sets, and the one other remote desktops read. */
		NSNumber *locked = session[@"CGSSessionScreenIsLocked"];
		if (locked && locked.boolValue) {
			return 0;
		}
		return CGDisplayIsAsleep(CGMainDisplayID()) ? 0 : 1;
	}
}

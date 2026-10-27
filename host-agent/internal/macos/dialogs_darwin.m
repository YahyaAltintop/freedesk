//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <os/lock.h>
#include <stdlib.h>
#include <string.h>

#include "_cgo_export.h"
#include "appkit_darwin.h"
#include "dialogs_darwin.h"

/* How often a dialog on screen looks at whether it has been withdrawn or has
 * run out of time. */
static const NSTimeInterval fd_dlg_tick = 0.1;

/* The tokens of dialogs withdrawn before they ended: on screen until their
 * next tick, or not shown yet. */
static os_unfair_lock fd_dlg_lock = OS_UNFAIR_LOCK_INIT;
static NSMutableSet<NSNumber *> *fd_dlg_withdrawn;

/* The alert on screen; main thread only. For tests. */
static NSAlert *fd_dlg_alert;

void fd_dlg_withdraw(uint64_t token) {
	@autoreleasepool {
		NSNumber *t = @(token);
		os_unfair_lock_lock(&fd_dlg_lock);
		if (!fd_dlg_withdrawn) {
			fd_dlg_withdrawn = [NSMutableSet set];
		}
		[fd_dlg_withdrawn addObject:t];
		os_unfair_lock_unlock(&fd_dlg_lock);
	}
}

static BOOL fd_dlg_is_withdrawn(uint64_t token) {
	NSNumber *t = @(token);
	os_unfair_lock_lock(&fd_dlg_lock);
	BOOL withdrawn = [fd_dlg_withdrawn containsObject:t];
	os_unfair_lock_unlock(&fd_dlg_lock);
	return withdrawn;
}

static void fd_dlg_forget(uint64_t token) {
	NSNumber *t = @(token);
	os_unfair_lock_lock(&fd_dlg_lock);
	[fd_dlg_withdrawn removeObject:t];
	os_unfair_lock_unlock(&fd_dlg_lock);
}

/* fd_dlg_on_main runs a dialog on the main thread. Not through the main
 * dispatch queue: a dialog runs a modal loop, and while one of the queue's
 * blocks runs the loop serves none of the others, so the status window would
 * stop changing until the dialog closed. In the default mode only, so never
 * in the middle of another modal loop, or of a drag. */
static void fd_dlg_on_main(void (^block)(void)) {
	CFRunLoopRef main = CFRunLoopGetMain();
	CFRunLoopPerformBlock(main, kCFRunLoopDefaultMode, block);
	CFRunLoopWakeUp(main);
}

/* fd_dlg_present makes sure a dialog is seen, without taking the keyboard:
 * above every other application's windows, on whichever Space the operator
 * is on, with the Dock icon bouncing until FreeDesk is brought forward.
 * FreeDesk is not made the active application, so a key the operator was
 * already typing elsewhere can never land on the dialog: they click it to
 * answer. Returns the attention request, to cancel once the dialog closes. */
static NSInteger fd_dlg_present(NSWindow *window) {
	window.level = NSFloatingWindowLevel;
	window.hidesOnDeactivate = NO;
	window.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorFullScreenAuxiliary;
	return [NSApp requestUserAttention:NSCriticalRequest];
}

/* fd_dlg_ticker calls check every tick while a dialog is up; check returns
 * YES once it has ended the dialog. Added for every mode, as a modal loop
 * runs in its own. */
static NSTimer *fd_dlg_ticker(BOOL (^check)(void)) {
	NSTimer *timer = [NSTimer timerWithTimeInterval:fd_dlg_tick repeats:YES block:^(NSTimer *t) {
		if (check()) {
			[t invalidate];
			fd_wake();
		}
	}];
	[NSRunLoop.currentRunLoop addTimer:timer forMode:NSRunLoopCommonModes];
	return timer;
}

/* fd_dlg_end hands how a dialog ended to Go. */
static void fd_dlg_end(uint64_t token, int result, NSArray<NSString *> *paths) {
	fd_dlg_forget(token);
	int n = (int)paths.count;
	const char **list = n > 0 ? calloc((size_t)n, sizeof *list) : NULL;
	if (n > 0 && !list) {
		n = 0;
		result = FD_DLG_CANCELLED;
	}
	for (int i = 0; i < n; i++) {
		list[i] = paths[(NSUInteger)i].UTF8String;
	}
	fdDialogEnded(token, result, (char **)list, n);
	free(list);
}

/* --- the Yes/No alert -------------------------------------------------------- */

/* fd_dlg_body is the question as written. Its line breaks are part of how it
 * reads (a list of files, one to a line), so it is not left to the alert,
 * which would set it in a narrow column of its own. */
static NSView *fd_dlg_body(NSString *text) {
	NSTextField *body = [NSTextField wrappingLabelWithString:text];
	body.font = [NSFont systemFontOfSize:NSFont.smallSystemFontSize];
	body.selectable = NO;
	body.preferredMaxLayoutWidth = 380;
	body.frame = NSMakeRect(0, 0, 380, body.fittingSize.height);
	return body;
}

static int fd_dlg_run_alert(uint64_t token, NSString *title, NSString *text, CFAbsoluteTime deadline) {
	if (fd_dlg_is_withdrawn(token)) {
		return FD_DLG_WITHDRAWN;
	}
	if (CFAbsoluteTimeGetCurrent() >= deadline) {
		return FD_DLG_TIMED_OUT;
	}
	NSAlert *alert = [[NSAlert alloc] init];
	alert.alertStyle = NSAlertStyleWarning;
	alert.messageText = title;
	alert.accessoryView = fd_dlg_body(text);
	/* The first button is the one Return presses: No, so that a key pressed
	 * out of habit never approves anything. */
	[alert addButtonWithTitle:@"No"];
	[alert addButtonWithTitle:@"Yes"];
	[alert layout];

	__block int ended = 0;
	NSTimer *timer = fd_dlg_ticker(^BOOL {
		if (fd_dlg_is_withdrawn(token)) {
			ended = FD_DLG_WITHDRAWN;
		} else if (CFAbsoluteTimeGetCurrent() >= deadline) {
			ended = FD_DLG_TIMED_OUT;
		} else {
			return NO;
		}
		[NSApp abortModal];
		return YES;
	});
	NSInteger attention = fd_dlg_present(alert.window);
	fd_dlg_alert = alert;
	NSModalResponse response = [alert runModal];
	fd_dlg_alert = nil;
	[timer invalidate];
	[alert.window orderOut:nil];
	if (attention != 0) {
		[NSApp cancelUserAttentionRequest:attention];
	}
	if (ended != 0) {
		return ended;
	}
	return response == NSAlertSecondButtonReturn ? FD_DLG_YES : FD_DLG_NO;
}

void fd_dlg_ask(uint64_t token, const char *title, const char *text, double timeout) {
	@autoreleasepool {
		NSString *t = fd_string(title), *body = fd_string(text);
		CFAbsoluteTime deadline = CFAbsoluteTimeGetCurrent() + timeout;
		fd_dlg_on_main(^{
			@autoreleasepool {
				fd_dlg_end(token, fd_dlg_run_alert(token, t, body, deadline), nil);
			}
		});
	}
}

/* --- the Open panel ----------------------------------------------------------- */

/* fd_dlg_choosable_url: a package, an application for one, is a folder the
 * Finder shows as a single file. Only files can be sent, so packages cannot
 * be chosen; folders still open, to choose files inside them. */
static BOOL fd_dlg_choosable_url(NSURL *url) {
	NSNumber *package = nil;
	[url getResourceValue:&package forKey:NSURLIsPackageKey error:NULL];
	return !package.boolValue;
}

@interface FDChooseFiles : NSObject <NSOpenSavePanelDelegate>
@end

@implementation FDChooseFiles
- (BOOL)panel:(id)sender shouldEnableURL:(NSURL *)url {
	return fd_dlg_choosable_url(url);
}
@end

static int fd_dlg_run_panel(uint64_t token, NSString *title, NSString *message, NSString *prompt, NSArray<NSString *> **chosen) {
	if (fd_dlg_is_withdrawn(token)) {
		return FD_DLG_WITHDRAWN;
	}
	NSOpenPanel *panel = [NSOpenPanel openPanel];
	FDChooseFiles *delegate = [[FDChooseFiles alloc] init];
	panel.delegate = delegate;
	panel.title = title;
	panel.message = message;
	panel.prompt = prompt;
	panel.canChooseFiles = YES;
	panel.canChooseDirectories = NO;
	panel.allowsMultipleSelection = YES;

	__block BOOL withdrawn = NO;
	NSTimer *timer = fd_dlg_ticker(^BOOL {
		if (!fd_dlg_is_withdrawn(token)) {
			return NO;
		}
		withdrawn = YES;
		[panel cancel:nil];
		return YES;
	});
	NSInteger attention = fd_dlg_present(panel);
	NSModalResponse response = [panel runModal];
	[timer invalidate];
	if (attention != 0) {
		[NSApp cancelUserAttentionRequest:attention];
	}
	panel.delegate = nil;
	if (withdrawn) {
		return FD_DLG_WITHDRAWN;
	}
	if (response != NSModalResponseOK) {
		return FD_DLG_CANCELLED;
	}
	NSMutableArray<NSString *> *paths = [NSMutableArray array];
	for (NSURL *url in panel.URLs) {
		/* Composed ("ü" one character), as names are typed and shown
		 * everywhere else; the file system finds a file under either form. */
		NSString *path = url.path.precomposedStringWithCanonicalMapping;
		if (path.length > 0) {
			[paths addObject:path];
		}
	}
	*chosen = paths;
	return FD_DLG_CHOSEN;
}

void fd_dlg_choose_files(uint64_t token, const char *title, const char *message, const char *prompt) {
	@autoreleasepool {
		NSString *t = fd_string(title), *m = fd_string(message), *p = fd_string(prompt);
		fd_dlg_on_main(^{
			@autoreleasepool {
				NSArray<NSString *> *paths = nil;
				int result = fd_dlg_run_panel(token, t, m, p, &paths);
				fd_dlg_end(token, result, paths);
			}
		});
	}
}

/* --- for tests ------------------------------------------------------------------- */

void fd_dlg_read_state(fd_dlg_state *s) {
	memset(s, 0, sizeof *s);
	dispatch_sync(dispatch_get_main_queue(), ^{
		NSWindow *window = NSApp.modalWindow;
		NSAlert *alert = fd_dlg_alert;
		s->shown = window.visible;
		s->level = (int)window.level;
		s->all_spaces = (window.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces) != 0;
		s->app_active = NSApp.active;
		fd_copy_c(s->title, sizeof s->title, alert ? alert.messageText : window.title);
		/* Return presses the window's default button. While the alert runs,
		 * AppKit keeps that there, not as the button's key equivalent. */
		NSButtonCell *byDefault = window.defaultButtonCell;
		for (NSButton *button in alert.buttons) {
			BOOL returns = byDefault ? button.cell == byDefault : [button.keyEquivalent isEqualToString:@"\r"];
			if (returns) {
				fd_copy_c(s->return_button, sizeof s->return_button, button.title);
			} else {
				fd_copy_c(s->other_button, sizeof s->other_button, button.title);
			}
		}
	});
}

void fd_dlg_press(const char *button) {
	@autoreleasepool {
		NSString *title = fd_string(button);
		dispatch_async(dispatch_get_main_queue(), ^{
			for (NSButton *b in fd_dlg_alert.buttons) {
				if ([b.title isEqualToString:title]) {
					[b performClick:nil];
					fd_wake();
					return;
				}
			}
		});
	}
}

int fd_dlg_choosable(const char *path) {
	@autoreleasepool {
		return fd_dlg_choosable_url([NSURL fileURLWithPath:fd_string(path)]) ? 1 : 0;
	}
}

int fd_dlg_snapshot(const char *path) {
	@autoreleasepool {
		__block int ok = 0;
		NSString *p = fd_string(path);
		dispatch_sync(dispatch_get_main_queue(), ^{
			ok = fd_snapshot(NSApp.modalWindow, p) ? 1 : 0;
		});
		return ok;
	}
}

//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <os/lock.h>

#include "_cgo_export.h"
#include "appkit_darwin.h"
#include "window_darwin.h"

/* Everything here runs on the main thread, except the fd_ functions Go calls
 * from its goroutines, which copy what they are given and hand it on. */

static NSString *const fd_starting = @"Starting…";

/* fd_app_stop ends fd_app_run. A dialog still modal (one the agent gave up
 * waiting for, see dialogs_darwin.m) would swallow the stop, which ends only
 * the modal session it finds; so that is ended first, and the stop follows
 * once it has gone, or after a few seconds regardless. */
static void fd_app_stop(void) {
	static int tries;
	if (NSApp.modalWindow && tries++ < 50) {
		[NSApp abortModal];
		fd_wake();
		dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 100 * NSEC_PER_MSEC), dispatch_get_main_queue(), ^{
			fd_app_stop();
		});
		return;
	}
	[NSApp stop:nil];
	fd_wake();
}

/* fd_note puts a line in the agent's log. */
static void fd_note(NSString *line) {
	fdStatusWindowNote((char *)line.UTF8String);
}

/* fd_open_url opens an address in the operator's browser. Every address it
 * is given was built by the agent from its configuration, never taken from
 * the network, so those are the only places it can point. */
static void fd_open_url(NSString *address) {
	if (address.length == 0) {
		return;
	}
	NSURL *url = [NSURL URLWithString:address];
	if (!url || ![[NSWorkspace sharedWorkspace] openURL:url]) {
		fd_note([@"could not open the browser for " stringByAppendingString:address]);
	}
}

/* A view that lays out from the top, as the Windows window does. */
@interface FDTopDownView : NSView
@end

@implementation FDTopDownView
- (BOOL)isFlipped {
	return YES;
}
@end

/* fd_text_view is a read-only, selectable text view in a scroll view. */
static NSTextView *fd_text_view(NSScrollView *scroll, NSFont *font) {
	NSSize size = scroll.contentSize;
	NSTextView *view = [[NSTextView alloc] initWithFrame:NSMakeRect(0, 0, size.width, size.height)];
	view.minSize = NSMakeSize(0, size.height);
	view.maxSize = NSMakeSize(FLT_MAX, FLT_MAX);
	view.verticallyResizable = YES;
	view.horizontallyResizable = NO;
	view.autoresizingMask = NSViewWidthSizable;
	view.textContainer.containerSize = NSMakeSize(size.width, FLT_MAX);
	view.textContainer.widthTracksTextView = YES;
	view.editable = NO;
	view.selectable = YES;
	view.richText = NO;
	view.font = font;
	view.textContainerInset = NSMakeSize(2, 4);
	scroll.documentView = view;
	return view;
}

@interface FDStatusWindow : NSObject <NSApplicationDelegate, NSWindowDelegate>
@property(strong) NSWindow *window;
@property(strong) NSWindow *licensesWindow;
@property(strong) NSTextField *codeLabel, *siteLabel, *statusLabel;
@property(strong) NSButton *codeCopyButton, *updateButton, *privacyButton, *licensesButton;
@property(strong) NSTextView *activity;
@property(strong) NSFont *monoFont;
@property(copy) NSString *code, *updateURL, *privacyURL, *licenses;
@property BOOL closing;     /* the operator asked to stop; the agent is stopping */
@property BOOL done;        /* the agent has finished */
@property BOOL failed;      /* it finished with an error, and says so */
@property BOOL terminating; /* Quit (or logging out) waits for the agent */
@property int exitCode;
@property NSUInteger copies; /* Copy code presses, so only the last one's "Copied" is undone */
@end

static FDStatusWindow *fd_window;

@implementation FDStatusWindow

- (NSTextField *)label:(NSString *)text frame:(NSRect)frame font:(NSFont *)font align:(NSTextAlignment)align {
	NSTextField *label = [NSTextField labelWithString:text];
	label.frame = frame;
	label.font = font;
	label.alignment = align;
	label.lineBreakMode = NSLineBreakByTruncatingTail;
	[self.window.contentView addSubview:label];
	return label;
}

- (NSButton *)button:(NSString *)title frame:(NSRect)frame action:(SEL)action {
	NSButton *button = [NSButton buttonWithTitle:title target:self action:action];
	button.frame = frame;
	button.bezelStyle = NSBezelStylePush;
	[self.window.contentView addSubview:button];
	return button;
}

/* The layout is the Windows window's, in points: a 460 x 428 window, the
 * code in the middle of its upper part, the activity log below. */
- (instancetype)initWithTitle:(NSString *)title licenses:(NSString *)licenses privacyURL:(NSString *)privacyURL {
	if (!(self = [super init])) {
		return nil;
	}
	self.licenses = licenses;
	self.privacyURL = privacyURL;
	NSRect frame = NSMakeRect(0, 0, 460, 428);
	self.window = [[NSWindow alloc] initWithContentRect:frame
	                                          styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable
	                                            backing:NSBackingStoreBuffered
	                                              defer:NO];
	self.window.title = title;
	self.window.releasedWhenClosed = NO;
	self.window.delegate = self;
	self.window.contentView = [[FDTopDownView alloc] initWithFrame:frame];

	NSFont *ui = [NSFont systemFontOfSize:NSFont.systemFontSize];
	self.monoFont = [NSFont monospacedSystemFontOfSize:11 weight:NSFontWeightRegular];

	NSTextField *caption = [self label:@"This computer's code" frame:NSMakeRect(20, 16, 420, 20) font:ui align:NSTextAlignmentCenter];
	caption.textColor = NSColor.secondaryLabelColor;
	self.codeLabel = [self label:fd_starting
	                       frame:NSMakeRect(20, 40, 420, 60)
	                        font:[NSFont systemFontOfSize:32 weight:NSFontWeightBold]
	                       align:NSTextAlignmentCenter];
	self.codeCopyButton = [self button:@"Copy code" frame:NSMakeRect(175, 108, 110, 28) action:@selector(putCodeOnClipboard:)];
	self.codeCopyButton.enabled = NO; /* nothing to copy yet */
	self.siteLabel = [self label:@"" frame:NSMakeRect(20, 148, 420, 20) font:ui align:NSTextAlignmentCenter];
	self.statusLabel = [self label:@"" frame:NSMakeRect(20, 172, 420, 20) font:ui align:NSTextAlignmentCenter];

	/* The update button has its own row, blank until there is something to
	 * offer; violet, the web page's accent, to stand out. AppKit shows that
	 * colour while the window is the active one, as it does the blue of a
	 * default button, and picks the title's colour to suit: a title in a
	 * colour of its own would be white on light grey the rest of the time. */
	self.updateButton = [self button:@"" frame:NSMakeRect(130, 196, 200, 30) action:@selector(openUpdate:)];
	self.updateButton.bezelColor = [NSColor colorWithSRGBRed:0x8b / 255.0 green:0x5c / 255.0 blue:0xf6 / 255.0 alpha:1];
	self.updateButton.font = [NSFont boldSystemFontOfSize:NSFont.systemFontSize];
	self.updateButton.hidden = YES;

	[self label:@"Activity" frame:NSMakeRect(20, 234, 200, 18) font:ui align:NSTextAlignmentLeft];
	/* What the program sends where, and the licenses of the code in it: small,
	 * at the right end of the Activity caption's row, out of the way. */
	CGFloat privacyX = licenses.length > 0 ? 274 : 360;
	if (privacyURL.length > 0) {
		self.privacyButton = [self button:@"Privacy" frame:NSMakeRect(privacyX, 229, 80, 23) action:@selector(openPrivacy:)];
		self.privacyButton.controlSize = NSControlSizeSmall;
	}
	if (licenses.length > 0) {
		self.licensesButton = [self button:@"Licenses" frame:NSMakeRect(360, 229, 80, 23) action:@selector(showLicenses:)];
		self.licensesButton.controlSize = NSControlSizeSmall;
	}

	NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:NSMakeRect(20, 254, 420, 160)];
	scroll.hasVerticalScroller = YES;
	scroll.borderType = NSBezelBorder;
	self.activity = fd_text_view(scroll, self.monoFont);
	[self.window.contentView addSubview:scroll];

	[self.window center];
	return self;
}

/* --- what Go asks for ------------------------------------------------------ */

- (void)showCode:(NSString *)code site:(NSString *)site {
	self.code = code;
	self.codeLabel.stringValue = code;
	self.siteLabel.stringValue = [@"Web page: " stringByAppendingString:site];
	if (!self.closing && !self.failed) {
		self.codeCopyButton.enabled = YES;
	}
}

- (void)showLog:(NSString *)text {
	self.activity.string = text;
	self.activity.font = self.monoFont;
	[self.activity scrollRangeToVisible:NSMakeRange(text.length, 0)];
}

- (void)showUpdate:(NSString *)version url:(NSString *)url {
	self.updateURL = url;
	self.updateButton.title = [@"Update to " stringByAppendingString:version];
	self.updateButton.hidden = NO;
}

/* finish is the agent having ended. A clean end, or one the operator asked
 * for, closes the window; a failure keeps it open, so the reason in the
 * activity log can be read. */
- (void)finish:(int)exitCode {
	self.done = YES;
	self.exitCode = exitCode;
	if (self.terminating) {
		[NSApp replyToApplicationShouldTerminate:YES];
		return;
	}
	if (self.closing || exitCode == 0) {
		[self.licensesWindow orderOut:nil];
		[self.window orderOut:nil];
		fd_app_stop();
		return;
	}
	self.failed = YES;
	self.codeLabel.stringValue = @"Could not start";
	self.codeLabel.textColor = NSColor.systemRedColor;
	self.statusLabel.stringValue = @"See the activity log below, then close this window.";
	self.codeCopyButton.enabled = NO;
}

/* requestClose is the operator's first request to stop: the close button, ⌘W,
 * Quit or logging out. The agent is asked to stop; the window goes only once
 * it has (finish), after it has removed its code and identity. */
- (void)requestClose {
	if (self.closing) {
		return;
	}
	self.closing = YES;
	self.statusLabel.stringValue = @"Stopping…";
	self.codeCopyButton.enabled = NO;
	[self.window standardWindowButton:NSWindowCloseButton].enabled = NO;
	fdStatusWindowCloseRequested();
}

/* --- the operator's clicks -------------------------------------------------- */

/* putCodeOnClipboard copies the code as shown ("738 - 986"): easy to read in
 * a message, and the web page ignores the spaces when it is pasted. */
- (void)putCodeOnClipboard:(id)sender {
	if (self.code.length == 0) {
		return;
	}
	NSPasteboard *pasteboard = NSPasteboard.generalPasteboard;
	[pasteboard clearContents];
	if (![pasteboard setString:self.code forType:NSPasteboardTypeString]) {
		fd_note(@"could not copy the code");
		return;
	}
	self.codeCopyButton.title = @"Copied";
	NSUInteger press = ++self.copies;
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.5 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
		if (press == self.copies) {
			self.codeCopyButton.title = @"Copy code";
		}
	});
}

- (void)openUpdate:(id)sender {
	fd_open_url(self.updateURL);
}

- (void)openPrivacy:(id)sender {
	fd_open_url(self.privacyURL);
}

/* showLicenses opens the license texts in a window of their own; pressed
 * again, it brings that window forward. Closing it leaves the agent alone. */
- (void)showLicenses:(id)sender {
	if (!self.licensesWindow) {
		NSRect frame = NSMakeRect(0, 0, 640, 480);
		NSWindow *window = [[NSWindow alloc] initWithContentRect:frame
		                                               styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
		                                                         NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
		                                                 backing:NSBackingStoreBuffered
		                                                   defer:NO];
		window.title = @"FreeDesk licenses";
		window.releasedWhenClosed = NO;
		NSScrollView *scroll = [[NSScrollView alloc] initWithFrame:frame];
		scroll.hasVerticalScroller = YES;
		scroll.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
		NSTextView *text = fd_text_view(scroll, self.monoFont);
		text.string = self.licenses;
		text.font = self.monoFont;
		window.contentView = scroll;
		[window center];
		self.licensesWindow = window;
	}
	[self.licensesWindow makeKeyAndOrderFront:nil];
}

/* --- the window and the application ----------------------------------------- */

- (BOOL)windowShouldClose:(NSWindow *)sender {
	if (sender != self.window) {
		return YES;
	}
	if (self.done) {
		[self.licensesWindow orderOut:nil];
		fd_app_stop();
		return YES;
	}
	[self requestClose];
	return NO;
}

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
	if (self.done) {
		return NSTerminateNow;
	}
	self.terminating = YES;
	[self requestClose];
	return NSTerminateLater;
}

/* AppKit ends the process itself once termination goes ahead, always with
 * status 0; an agent that had failed ends with its own. */
- (void)applicationWillTerminate:(NSNotification *)notification {
	if (self.exitCode != 0) {
		exit(self.exitCode);
	}
}

/* A click on the Dock icon brings the window back from the Dock. */
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)visible {
	[self.window makeKeyAndOrderFront:nil];
	return YES;
}

@end

/* fd_menu is the menu bar: Quit and Hide, Copy and Select All for the
 * activity log, and the window's Minimize and Close. */
static void fd_menu(NSString *name) {
	NSMenu *bar = [[NSMenu alloc] init];

	NSMenu *app = [[NSMenu alloc] init];
	[app addItemWithTitle:[@"Hide " stringByAppendingString:name] action:@selector(hide:) keyEquivalent:@"h"];
	[app addItem:NSMenuItem.separatorItem];
	[app addItemWithTitle:[@"Quit " stringByAppendingString:name] action:@selector(terminate:) keyEquivalent:@"q"];
	[bar addItemWithTitle:@"" action:nil keyEquivalent:@""].submenu = app;

	NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
	[edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
	[edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
	[bar addItemWithTitle:@"Edit" action:nil keyEquivalent:@""].submenu = edit;

	NSMenu *window = [[NSMenu alloc] initWithTitle:@"Window"];
	[window addItemWithTitle:@"Minimize" action:@selector(performMiniaturize:) keyEquivalent:@"m"];
	[window addItemWithTitle:@"Close" action:@selector(performClose:) keyEquivalent:@"w"];
	[bar addItemWithTitle:@"Window" action:nil keyEquivalent:@""].submenu = window;

	NSApp.mainMenu = bar;
	NSApp.windowsMenu = window;
}

int fd_win_open(const char *title, const char *licenses, const char *privacy_url, int visible, char *err, int errlen) {
	@autoreleasepool {
		if (![NSThread isMainThread]) {
			snprintf(err, (size_t)errlen, "the window must be opened on the main thread");
			return 0;
		}
		if (fd_window && !fd_window.done) {
			snprintf(err, (size_t)errlen, "the window is already open");
			return 0;
		}
		/* Without a window server session (a login over ssh, say) AppKit
		 * cannot start at all, and would end the process trying. */
		CFDictionaryRef session = CGSessionCopyCurrentDictionary();
		if (!session) {
			snprintf(err, (size_t)errlen, "no window server session");
			return 0;
		}
		CFRelease(session);

		/* A hidden window (tests) belongs to an application with no Dock
		 * icon, which never comes forward. */
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:visible ? NSApplicationActivationPolicyRegular : NSApplicationActivationPolicyAccessory];
		NSString *name = fd_string(title);
		fd_window = [[FDStatusWindow alloc] initWithTitle:name licenses:fd_string(licenses) privacyURL:fd_string(privacy_url)];
		NSApp.delegate = fd_window;
		fd_menu(name);
		if (visible) {
			[fd_window.window makeKeyAndOrderFront:nil];
			if (@available(macOS 14.0, *)) {
				[NSApp activate];
			} else {
				[NSApp activateIgnoringOtherApps:YES];
			}
		}
		return 1;
	}
}

int fd_app_run(void) {
	[NSApp run];
	return fd_window.exitCode;
}

void fd_win_set_code(const char *code, const char *site) {
	@autoreleasepool {
		NSString *c = fd_string(code), *s = fd_string(site);
		dispatch_async(dispatch_get_main_queue(), ^{
			[fd_window showCode:c site:s];
		});
	}
}

void fd_win_set_status(const char *text) {
	@autoreleasepool {
		NSString *t = fd_string(text);
		dispatch_async(dispatch_get_main_queue(), ^{
			fd_window.statusLabel.stringValue = t;
		});
	}
}

/* The log is set whole. A burst of lines would otherwise queue a burst of
 * whole texts; only the newest is kept, and set once. */
static os_unfair_lock fd_log_lock = OS_UNFAIR_LOCK_INIT;
static NSString *fd_log_pending;
static BOOL fd_log_scheduled;

void fd_win_set_log(const char *text) {
	@autoreleasepool {
		NSString *t = fd_string(text);
		os_unfair_lock_lock(&fd_log_lock);
		fd_log_pending = t;
		BOOL schedule = !fd_log_scheduled;
		fd_log_scheduled = YES;
		os_unfair_lock_unlock(&fd_log_lock);
		if (!schedule) {
			return;
		}
		dispatch_async(dispatch_get_main_queue(), ^{
			os_unfair_lock_lock(&fd_log_lock);
			NSString *latest = fd_log_pending;
			fd_log_pending = nil;
			fd_log_scheduled = NO;
			os_unfair_lock_unlock(&fd_log_lock);
			[fd_window showLog:latest];
		});
	}
}

void fd_win_show_update(const char *version, const char *url) {
	@autoreleasepool {
		NSString *v = fd_string(version), *u = fd_string(url);
		dispatch_async(dispatch_get_main_queue(), ^{
			[fd_window showUpdate:v url:u];
		});
	}
}

void fd_win_finish(int exit_code) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[fd_window finish:exit_code];
	});
}

void fd_win_read_state(fd_win_state *s) {
	dispatch_sync(dispatch_get_main_queue(), ^{
		FDStatusWindow *w = fd_window;
		fd_copy_c(s->code, sizeof s->code, w.codeLabel.stringValue);
		fd_copy_c(s->site, sizeof s->site, w.siteLabel.stringValue);
		fd_copy_c(s->status, sizeof s->status, w.statusLabel.stringValue);
		fd_copy_c(s->update_title, sizeof s->update_title, w.updateButton.title);
		s->copy_enabled = w.codeCopyButton.enabled;
		s->close_enabled = [w.window standardWindowButton:NSWindowCloseButton].enabled;
		s->update_visible = !w.updateButton.hidden;
		s->has_privacy = w.privacyButton != nil;
		s->has_licenses = w.licensesButton != nil;
		s->privacy_x = w.privacyButton.frame.origin.x;
		s->privacy_y = w.privacyButton.frame.origin.y;
		s->licenses_x = w.licensesButton.frame.origin.x;
		s->licenses_y = w.licensesButton.frame.origin.y;
		s->closing = w.closing;
		s->done = w.done;
		s->failed = w.failed;
		s->log_length = (int)w.activity.string.length;
		NSString *log = w.activity.string;
		NSRange last = [log lineRangeForRange:NSMakeRange(log.length, 0)];
		fd_copy_c(s->log_last, sizeof s->log_last, [log substringWithRange:last]);
		s->visible = w.window.visible;
		s->licenses_shown = w.licensesWindow.visible;
		NSTextView *licenses = (NSTextView *)((NSScrollView *)w.licensesWindow.contentView).documentView;
		s->licenses_length = (int)licenses.string.length;
	});
}

int fd_win_snapshot(const char *path) {
	@autoreleasepool {
		__block int ok = 0;
		NSString *p = fd_string(path);
		dispatch_sync(dispatch_get_main_queue(), ^{
			ok = fd_snapshot(fd_window.window, p) ? 1 : 0;
		});
		return ok;
	}
}

/* The close button's path, without its click: performClose: beeps once the
 * button is disabled, and a test is not to make a sound. */
void fd_win_request_close(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *window = fd_window.window;
		if ([fd_window windowShouldClose:window]) {
			[window close];
		}
	});
}

void fd_win_press_licenses(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[fd_window showLicenses:nil];
	});
}

void fd_win_close_licenses(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[fd_window.licensesWindow performClose:nil];
	});
}

void fd_app_set_appearance(int which) {
	dispatch_sync(dispatch_get_main_queue(), ^{
		NSAppearanceName name = which == 1 ? NSAppearanceNameAqua : which == 2 ? NSAppearanceNameDarkAqua : nil;
		NSApp.appearance = name ? [NSAppearance appearanceNamed:name] : nil;
	});
}

int fd_win_open_from_tests(const char *title, const char *licenses, const char *privacy_url, int visible, char *err, int errlen) {
	__block int ok = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{
		ok = fd_win_open(title, licenses, privacy_url, visible, err, errlen);
	});
	return ok;
}

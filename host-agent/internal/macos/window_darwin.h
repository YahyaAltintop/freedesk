//go:build darwin && cgo

/*
 * The agent's window: this computer's code, a status line, an update button
 * and the activity log, with the application and its menu around it. AppKit
 * works on the main thread only. fd_win_open and fd_app_run are called on it;
 * every other function may be called from any thread, copies what it is
 * given, and hands the work to the main queue. A plain C interface over
 * Objective-C (window_darwin.m), which window_darwin.go wraps for Go.
 */
#ifndef FREEDESK_WINDOW_DARWIN_H
#define FREEDESK_WINDOW_DARWIN_H

/* Creates the application and the window, and with visible shows them. Main
 * thread only. Returns 0, with a message in err, when it cannot. */
int fd_win_open(const char *title, const char *licenses, const char *privacy_url, int visible, char *err, int errlen);

/* Runs the application until the window is done with; returns the exit code
 * fd_win_finish was given. Main thread only. */
int fd_app_run(void);

void fd_win_set_code(const char *code, const char *site);
void fd_win_set_status(const char *text);
/* Replaces the activity log. Calls in quick succession are applied once,
 * with the last text. */
void fd_win_set_log(const char *text);
void fd_win_show_update(const char *version, const char *url);
/* The agent has finished with this exit code. */
void fd_win_finish(int exit_code);

/* For tests: what the window shows, read on the main thread. */
typedef struct {
	char code[64];
	char site[256];
	char status[256];
	char update_title[64];
	int copy_enabled, close_enabled, update_visible;
	int has_privacy, has_licenses;
	double privacy_x, privacy_y, licenses_x, licenses_y;
	int closing, done, failed;
	int log_length;
	char log_last[128]; /* the activity log's last line */
	int visible;
	int licenses_shown, licenses_length;
} fd_win_state;

void fd_win_read_state(fd_win_state *s);
/* For tests: the window's contents as a PNG file. */
int fd_win_snapshot(const char *path);
/* For tests: the close button, pressed. */
void fd_win_request_close(void);
/* For tests: the Licenses button pressed, and the licenses window closed. */
void fd_win_press_licenses(void);
void fd_win_close_licenses(void);
/* For tests: draws everything light (1), dark (2), or as the system does (0). */
void fd_app_set_appearance(int which);
/* For tests: fd_win_open, run on the main thread from any other. A window
 * the agent is done with may be replaced by a new one. */
int fd_win_open_from_tests(const char *title, const char *licenses, const char *privacy_url, int visible, char *err, int errlen);

#endif

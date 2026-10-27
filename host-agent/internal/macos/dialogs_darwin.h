//go:build darwin && cgo

/*
 * The operator's dialogs: a Yes/No alert, and the Open panel files to send
 * are chosen in. Both run on the main thread, in the loop fd_app_run runs,
 * without making FreeDesk the active application. The functions here may be
 * called from any thread and return at once; how a dialog ended comes back
 * through fdDialogEnded (dialogs_darwin.go), on the main thread, with the
 * token it was started under.
 */
#ifndef FREEDESK_DIALOGS_DARWIN_H
#define FREEDESK_DIALOGS_DARWIN_H

#include <stdint.h>

enum {
	FD_DLG_YES = 1,       /* the operator pressed Yes */
	FD_DLG_NO = 2,        /* the operator pressed No */
	FD_DLG_TIMED_OUT = 3, /* nobody answered in time */
	FD_DLG_WITHDRAWN = 4, /* fd_dlg_withdraw closed it, or kept it from opening */
	FD_DLG_CHOSEN = 5,    /* files were chosen; their paths come with it */
	FD_DLG_CANCELLED = 6, /* the operator cancelled the panel */
};

/* Asks a Yes/No question: title in bold, text below it as written. No is the
 * button Return presses. Nobody answering within timeout seconds of this call
 * ends it as FD_DLG_TIMED_OUT. */
void fd_dlg_ask(uint64_t token, const char *title, const char *text, double timeout);
/* Lets the operator choose files, any number, folders excluded. */
void fd_dlg_choose_files(uint64_t token, const char *title, const char *message, const char *prompt);
/* Closes the dialog started under token, or keeps it from opening. */
void fd_dlg_withdraw(uint64_t token);

/* For tests: the dialog on screen, read on the main thread. */
typedef struct {
	int shown;      /* a dialog is up and visible */
	int level;      /* its window level */
	int all_spaces; /* it shows on every Space */
	int app_active; /* FreeDesk is the active application */
	char title[128];
	char return_button[32]; /* the button Return presses */
	char other_button[32];
} fd_dlg_state;

void fd_dlg_read_state(fd_dlg_state *s);
/* For tests: presses the alert's button with that title. */
void fd_dlg_press(const char *button);
/* For tests: whether the Open panel lets the file at path be chosen. */
int fd_dlg_choosable(const char *path);
/* For tests: the dialog on screen as a PNG file. */
int fd_dlg_snapshot(const char *path);

#endif

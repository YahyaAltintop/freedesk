FreeDesk host agent
===================

This folder lets someone else see and control THIS computer from a browser.

1. Double-click freedesk-host.exe. A console window opens and shows a
   9-digit code (it changes every time you start the program).
   Windows may ask you to allow network access for the program: allow it
   (WebRTC needs to send video directly to the other computer).
2. Tell the code to the person who should connect. They open the FreeDesk
   web page in their browser, type the code and press Connect.
3. A window appears on this computer asking whether to allow the connection.
   Click Yes. Nothing is ever allowed without your click; if you do not
   answer within 45 seconds the request is rejected.
4. Close the console window (or press Ctrl+C in it) to stop sharing. The code
   stops working immediately.

If three connection requests in a row are not approved, the program assumes
the code has reached the wrong people: it stops using it and prints a new
one. Anyone you were expecting needs the new code.

If the window shows an error instead of a code, it also says what to fix;
the window stays open until you press Enter.

If the other person sends you files, a second window asks first. Files you
accept are saved to your Downloads\FreeDesk folder. Nothing is written
without your click, and an existing file is never replaced. While that window
is open the other person's mouse and keyboard are paused, so the click can
only be yours. If you step away
and three of those windows go unanswered, the program stops asking for the
rest of that connection, so you never come back to a pile of them.

If they ask you for files, a file picker opens on this computer. Only what
you choose there is sent, and cancelling sends nothing. Their mouse and
keyboard are paused while it is open, for the same reason.

While you are connected, text you copy is shared with the other person and
theirs with you, so Ctrl+C and Ctrl+V work across both computers. The window
that asks you to accept the connection says so. Text copied from a password
manager is skipped, and you can switch sharing off entirely by putting
RC_CLIPBOARD=off in the .env file next to the program.

Files work the same way: files the other person pastes are saved to your
Downloads\FreeDesk folder (after you accept) and put on your clipboard, so
Ctrl+V in Explorer pastes them. Files you copy are offered to them, but
nothing is sent until they ask for it.

Files:
  freedesk-host.exe   the agent (needs no installation)
  ffmpeg.exe          screen capture and video encoding (unmodified official
                      build; see ffmpeg-LICENSE.txt). Keep it next to the exe.
  LICENSE.txt         FreeDesk license (MIT)

Limits of this version: Windows 10/11 only, primary monitor only, no audio,
the clipboard carries text and files but not images or formatting, folders
cannot be sent (only files), a transfer that is interrupted starts over rather
than carrying on where it stopped, no unattended access (someone must click
Yes), and some networks (mobile / CGNAT) cannot connect directly.

Source code and the web page address: see the project's GitHub page.

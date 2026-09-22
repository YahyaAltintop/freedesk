FreeDesk host
=============

This folder lets someone else see and control THIS computer from a browser.

1. Double-click freedesk.exe. A small window opens and shows a 6-digit
   code (it changes every time you start the program). "Copy code" puts it
   on the clipboard.
   The first time, Windows may say "Windows protected your PC" and
   "Unknown publisher": the program is not signed yet. Click "More info",
   then "Run anyway". To be sure of what you downloaded, compare the zip's
   SHA-256 with the .sha256 file on the release page.
   As soon as it starts, Windows may ask whether to allow the program to use
   the network: click "Allow access" with both Private and Public ticked. If
   that question is cancelled or missed, nobody can connect to this computer;
   the window then says so and tells you where in Windows Security to fix it.
2. Tell the code to the person who should connect. They open the FreeDesk
   web page in their browser, type the code and press Connect.
3. A window appears on this computer asking whether to allow the connection.
   Click Yes. Nothing is ever allowed without your click; if you do not
   answer within 45 seconds the request is rejected.
4. Close the FreeDesk window to stop sharing. The code stops working
   immediately.

The window's "Activity" box lists what the program does. If it cannot start,
the reason is written there together with what to fix, and the window stays
open until you close it.

When a newer version has been published, a purple "Update to ..." button
appears in the window; it opens the download page in your browser. The
program never updates itself: you download the new zip and replace this
folder. (The program asks GitHub about this once when it starts; put
RC_UPDATE_CHECK=off in a file named .env next to it to stop that.)

If three connection requests in a row are not approved, the program assumes
the code has reached the wrong people: it stops using it and shows a new
one. Anyone you were expecting needs the new code.

If the other person sends you files, a second window asks first. Files you
accept are saved to your Downloads\FreeDesk folder. Nothing is written
without your click, and an existing file is never replaced. While that window
is open the other person's mouse and keyboard are paused, so the click can
only be yours. If you step away and three of those windows go unanswered,
the program stops asking for the rest of that connection, so you never come
back to a pile of them.

If they ask you for files, a file picker opens on this computer. Only what
you choose there is sent, and cancelling sends nothing. Their mouse and
keyboard are paused while it is open, for the same reason.

While you are connected, text you copy is shared with the other person and
theirs with you, so Ctrl+C and Ctrl+V work across both computers. The window
that asks you to accept the connection says so. Text copied from a password
manager is skipped, and you can switch sharing off entirely by putting
RC_CLIPBOARD=off in a file named .env next to the program.

Files work the same way: files the other person pastes are saved to your
Downloads\FreeDesk folder (after you accept) and put on your clipboard, so
Ctrl+V in Explorer pastes them. Files you copy are offered to them, but
nothing is sent until they ask for it.

Files in this folder:
  freedesk.exe        the program (needs no installation)
  ffmpeg\ffmpeg.exe   screen capture and video encoding (unmodified official
                      build; its license is ffmpeg\LICENSE.txt). Keep the
                      ffmpeg folder next to freedesk.exe; you never run it
                      yourself.
  README.txt          this file
  LICENSE.txt         FreeDesk license (MIT)

Limits of this version: Windows 10/11 only, primary monitor only, no audio,
the clipboard carries text and files but not images or formatting, folders
cannot be sent (only files), a transfer that is interrupted starts over rather
than carrying on where it stopped, no unattended access (someone must click
Yes), and some networks (mobile / CGNAT) cannot connect directly.

Source code and the web page address: https://github.com/YahyaAltintop/freedesk

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

If the window shows an error instead of a code, it also says what to fix;
the window stays open until you press Enter.

Files:
  freedesk-host.exe   the agent (needs no installation)
  ffmpeg.exe          screen capture and video encoding (unmodified official
                      build; see ffmpeg-LICENSE.txt). Keep it next to the exe.
  LICENSE.txt         FreeDesk license (MIT)

Limits of this version: Windows 10/11 only, primary monitor only, no audio,
no clipboard or file transfer, no unattended access (someone must click Yes),
and some networks (mobile / CGNAT) cannot connect directly.

Source code and the web page address: see the project's GitHub page.

# vdisplay

Give a headless Windows PC a virtual monitor at a real resolution.

With no monitor attached, Windows runs the console at 640×480. Browser automation gets a tiny viewport, remote-control tools show a postage stamp, and dialogs taller than the screen hide their buttons. An HDMI dummy plug fixes it in hardware; vdisplay fixes it in software.

It drives [Parsec's virtual display driver](https://parsec.app) (parsec-vdd), which is free and signed by Parsec Cloud, Inc., so nothing has to be added to the machine's trusted certificates. vdisplay adds one virtual display, makes it the only active screen ("second screen only") at the size you choose, and keeps it alive with the driver's keep-alive pings for as long as it runs.

## Install

1. Install the driver, as administrator: download `parsec-vdd-0.45.0.0.exe` from `https://builds.parsec.app/vdd/parsec-vdd-0.45.0.0.exe`, check its signature (`Get-AuthenticodeSignature` should show Valid, signer Parsec Cloud, Inc.), and run it (`/S` installs silently). Device Manager then lists "Parsec Virtual Display Adapter".
2. Build vdisplay with Go 1.26 or later: `go build -o vdisplay.exe .`
3. From an elevated PowerShell: `powershell -NoProfile -ExecutionPolicy Bypass -File register-task.ps1` (options: `-Width`, `-Height`, `-Exe`, `-Log`, `-TaskName`, `-TaskPath`). This registers a logon task with highest privileges that starts vdisplay, plus a five-minute trigger that restarts it if it stops.

Check it with `vdisplay -status`, which prints the active screens, for example `\.\DISPLAY7 1920x1080 primary=true (Parsec Virtual Display Adapter)`.

## Notes

- It needs administrator rights to open the driver.
- If a Parsec display is already attached when it starts (left by an earlier run), it reuses that one instead of adding a second.
- When you connect over Remote Desktop, Windows uses the RDP session's own display as usual; the virtual one takes over again for the console.
- Stopping vdisplay with Ctrl+C removes the display it added; killing it leaves the display until the driver drops it.
- Tested on Windows 11 Pro (build 26300) with an AMD Radeon iGPU and parsec-vdd 0.45.

## Credits

The driver protocol (device interface GUID, IOCTL codes and buffers) follows the header published by [nomi-san/parsec-vdd](https://github.com/nomi-san/parsec-vdd) under the MIT License. Parsec and parsec-vdd belong to Parsec Cloud, Inc.; this project only talks to the installed driver and doesn't redistribute it.

## License

MIT

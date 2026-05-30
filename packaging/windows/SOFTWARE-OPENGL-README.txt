sd2snes Covers - software OpenGL version
=========================================

WHEN DO I NEED THIS?
--------------------
Use this version if the normal sd2snes-covers-windows-amd64.exe does not open -
it shows up in Task Manager but no window appears, or the window flashes and
closes immediately. That means your machine has no working OpenGL driver,
typically:

  * display adapter shown as "Microsoft Basic Render Driver" (no GPU driver)
  * Remote Desktop (RDP) sessions
  * virtual machines without 3D acceleration

HOW TO USE
----------
Keep BOTH files together in the same folder:

    sd2snes Covers.exe
    opengl32.dll

Then just run "sd2snes Covers.exe" (double-click). That is all - no launcher,
no settings.

The opengl32.dll next to the .exe is a software OpenGL renderer (Mesa /
llvmpipe); Windows loads it automatically because it sits next to the program.
Rendering runs on the CPU, which is perfectly fine for this app. Do NOT move
the .exe away from the .dll.

If your machine actually has a GPU, the cleaner fix is to install its driver
(Intel / AMD / NVIDIA) and use the normal .exe instead.

ABOUT opengl32.dll
------------------
Mesa3D software OpenGL for Windows (llvmpipe), the "MesaForWindows" build from
https://fdossena.com/?p=mesa/index.frag . Mesa3D is MIT-licensed.

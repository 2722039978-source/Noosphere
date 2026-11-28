package main

import "golang.org/x/sys/windows"

func init() {
	// This background process reports failures through its logs and task events.
	// Its command children inherit the mode. The interactive app and OS settings
	// are separate processes.
	getErrorMode := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetErrorMode")
	current, _, _ := getErrorMode.Call()
	windows.SetErrorMode(uint32(current) | windows.SEM_FAILCRITICALERRORS | windows.SEM_NOGPFAULTERRORBOX | windows.SEM_NOOPENFILEERRORBOX)
}

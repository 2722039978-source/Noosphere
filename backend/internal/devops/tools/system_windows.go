package tools

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func diagnosticEnvironment() []string {
	dir, _ := windows.GetSystemDirectory()
	return []string{"SystemRoot=" + filepath.Dir(dir), "WINDIR=" + filepath.Dir(dir), "PATH=" + dir}
}

func diagnosticCommand(name string, args []string) (string, []string) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return "", nil
	}
	switch name {
	case "system_info":
		return filepath.Join(dir, "systeminfo.exe"), nil
	case "tasklist":
		return filepath.Join(dir, "tasklist.exe"), []string{"/FO", "CSV", "/NH"}
	case "netstat":
		return filepath.Join(dir, "netstat.exe"), []string{"-ano"}
	case "service_list":
		return filepath.Join(dir, "sc.exe"), []string{"query", "state=", "all"}
	case "service_status":
		return filepath.Join(dir, "sc.exe"), append([]string{"query"}, args...)
	}
	return "", nil
}

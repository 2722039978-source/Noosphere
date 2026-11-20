package tools

func diagnosticEnvironment() []string {
	return []string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
}

func diagnosticCommand(name string, args []string) (string, []string) {
	switch name {
	case "system_info":
		return "/usr/bin/sw_vers", nil
	case "tasklist":
		return "/bin/ps", []string{"-axo", "pid,ppid,comm"}
	case "netstat":
		return "/usr/sbin/netstat", []string{"-an"}
	case "service_list":
		return "/bin/launchctl", []string{"list"}
	case "service_status":
		return "/bin/launchctl", append([]string{"list"}, args...)
	}
	return "", nil
}

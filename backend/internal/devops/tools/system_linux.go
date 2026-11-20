package tools

func diagnosticEnvironment() []string {
	return []string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin", "SYSTEMD_COLORS=0"}
}

func diagnosticCommand(name string, args []string) (string, []string) {
	switch name {
	case "system_info":
		return "/usr/bin/uname", []string{"-srm"}
	case "tasklist":
		return "/usr/bin/ps", []string{"-eo", "pid,ppid,comm"}
	case "netstat":
		return "/usr/bin/ss", []string{"-tuna"}
	case "service_list":
		return "/usr/bin/systemctl", []string{"list-units", "--all", "--type=service", "--plain", "--no-legend", "--no-pager"}
	case "service_status":
		return "/usr/bin/systemctl", append([]string{"show", "--no-pager", "--property=Id,LoadState,ActiveState,SubState,UnitFileState", "--"}, args...)
	}
	return "", nil
}

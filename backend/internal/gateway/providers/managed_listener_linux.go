package providers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func managedPeerMatches(ctx context.Context, pid uint32, serverPort, clientPort int) bool {
	f, err := os.Open("/proc/net/tcp")
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	local := fmt.Sprintf("0100007F:%04X", serverPort)
	remote := fmt.Sprintf("0100007F:%04X", clientPort)
	var socket string
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 10 && fields[1] == local && fields[2] == remote && fields[3] == "01" && fields[9] != "0" {
			if socket != "" {
				return false
			}
			socket = "socket:[" + fields[9] + "]"
		}
	}
	if socket == "" || scanner.Err() != nil {
		return false
	}
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	fds, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, fd := range fds {
		if ctx.Err() != nil {
			return false
		}
		if target, _ := os.Readlink(filepath.Join(dir, fd.Name())); target == socket {
			return true
		}
	}
	return false
}

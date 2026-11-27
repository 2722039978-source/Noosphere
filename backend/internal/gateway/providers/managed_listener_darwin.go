package providers

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

func managedPeerMatches(ctx context.Context, pid uint32, serverPort, clientPort int) bool {
	process := strconv.FormatUint(uint64(pid), 10)
	local := "127.0.0.1:" + strconv.Itoa(serverPort)
	remote := "127.0.0.1:" + strconv.Itoa(clientPort)
	out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-p", process, "-i4TCP@"+local, "-sTCP:ESTABLISHED", "-Fpn").Output()
	if err != nil {
		return false
	}
	owned := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "p") {
			owned = line == "p"+process
		}
		if owned && strings.TrimSuffix(line, " (ESTABLISHED)") == "n"+local+"->"+remote {
			return true
		}
	}
	return false
}

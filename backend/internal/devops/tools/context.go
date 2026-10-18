package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type cappedOutput struct{ bytes.Buffer }

func (b *cappedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 128*1024 - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.Buffer.Write(p)
	}
	return n, nil
}

func commandResult(ctx context.Context, limit int, name string, args ...string) ToolResult {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out cappedOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ToolResult{Error: err.Error()}
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) > limit {
		lines = lines[:limit]
	}
	return ToolResult{Success: true, Output: strings.Join(lines, "\n")}
}

func ExecSystemInfoContext(ctx context.Context) ToolResult {
	return commandResult(ctx, 200, "systeminfo")
}
func ExecTaskListContext(ctx context.Context) ToolResult {
	return commandResult(ctx, 20, "tasklist", "/FO", "CSV", "/NH")
}
func ExecNetStatContext(ctx context.Context) ToolResult {
	return commandResult(ctx, 30, "netstat", "-ano")
}
func ExecServiceListContext(ctx context.Context) ToolResult {
	return commandResult(ctx, 100, "sc", "query", "state=", "all")
}
func ExecServiceStatusContext(ctx context.Context, name string) ToolResult {
	return commandResult(ctx, 50, "sc", "query", name)
}

func readLog(ctx context.Context, path string, lines int, keyword string, search bool) ToolResult {
	f, err := os.Open(path)
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ToolResult{Error: err.Error()}
	}
	if !info.Mode().IsRegular() || info.Size() > 8*1024*1024 {
		return ToolResult{Error: "log must be a regular file no larger than 8 MB"}
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if lines < 1 || lines > 200 {
		return ToolResult{Error: "invalid line limit"}
	}
	result := make([]string, 0, lines)
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return ToolResult{Error: err.Error()}
		}
		line := scanner.Text()
		if len(line) > 4096 {
			line = line[:4096] + " [truncated]"
		}
		if search {
			if !strings.Contains(strings.ToLower(line), strings.ToLower(keyword)) {
				continue
			}
			result = append(result, line)
			if len(result) >= lines {
				break
			}
		} else {
			if len(result) == lines {
				copy(result, result[1:])
				result = result[:len(result)-1]
			}
			result = append(result, line)
		}
	}
	if err = scanner.Err(); err != nil {
		return ToolResult{Error: fmt.Sprintf("read log: %v", err)}
	}
	return ToolResult{Success: true, Output: strings.Join(result, "\n")}
}
func ExecTailLogContext(ctx context.Context, path string, lines int) ToolResult {
	return readLog(ctx, path, lines, "", false)
}
func ExecSearchLogContext(ctx context.Context, path, keyword string) ToolResult {
	return readLog(ctx, path, 50, keyword, true)
}

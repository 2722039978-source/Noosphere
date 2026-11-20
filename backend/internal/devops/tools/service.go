package tools

import "context"

func ExecServiceList() ToolResult {
	return ExecServiceListContext(context.Background())
}

func ExecServiceStatus(name string) ToolResult {
	return ExecServiceStatusContext(context.Background(), name)
}

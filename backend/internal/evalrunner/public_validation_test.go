package evalrunner

import (
	"strings"
	"testing"
)

func TestPublicSourceValidationAcceptsFiniteSignatures(t *testing.T) {
	for _, typ := range []string{"bool", "string", "int", "int64", "float64", "[]bool", "[]string", "[]int", "[]int64", "[]float64"} {
		t.Run(typ, func(t *testing.T) {
			spec := CompileSpec{Source: "package main\nfunc Solve(value " + typ + ") " + typ + " { return value }", FunctionName: "Solve", InputType: typ, OutputType: typ}
			if err := ValidateCompileSpec(spec); err != nil {
				t.Fatalf("有效函数未通过校验: %v", err)
			}
		})
	}
	for _, source := range []string{
		"package main\nfunc Solve(value int) int { if value <= 0 { return 0 }; return value + Solve(value-1) }",
		"package main\nfunc Solve(value int) (result int) { result = value + 1; return }",
	} {
		if err := ValidateCompileSpec(CompileSpec{Source: source, FunctionName: "Solve", InputType: "int", OutputType: "int"}); err != nil {
			t.Fatalf("递归或具名返回值未通过校验: %v", err)
		}
	}
}

func TestPublicSourceValidationRejectsInvalidPrograms(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"syntax", "package main\nfunc Solve(value int) int { return } ???"},
		{"package", "package other\nfunc Solve(value int) int { return value }"},
		{"import", "package main\nimport \"os\"\nfunc Solve(value int) int { os.Exit(0); return value }"},
		{"global", "package main\nvar state = 1\nfunc Solve(value int) int { return value }"},
		{"additional_function", "package main\nfunc Solve(value int) int { return value }; func Extra() {}"},
		{"wrong_name", "package main\nfunc Other(value int) int { return value }"},
		{"argument_count", "package main\nfunc Solve(a, b int) int { return a + b }"},
		{"argument_type", "package main\nfunc Solve(value int64) int { return int(value) }"},
		{"return_type", "package main\nfunc Solve(value int) int64 { return int64(value) }"},
		{"multiple_returns", "package main\nfunc Solve(value int) (int, int) { return value, value }"},
		{"variadic", "package main\nfunc Solve(value ...int) int { return len(value) }"},
		{"generic", "package main\nfunc Solve[T any](value int) int { return value }"},
		{"method", "package main\nfunc (receiver int) Solve(value int) int { return value }"},
		{"compiler_directive", "package main\n//go:nosplit\nfunc Solve(value int) int { return value }"},
		{"line_directive", "package main\n//line other.go:1\nfunc Solve(value int) int { return value }"},
		{"goroutine", "package main\nfunc Solve(value int) int { go Solve(value); return value }"},
		{"defer", "package main\nfunc Solve(value int) int { defer Solve(value); return value }"},
		{"function_literal", "package main\nfunc Solve(value int) int { fn := func() int { return value }; return fn() }"},
		{"indirect_call", "package main\nfunc Solve(value int) int { fn := Solve; return fn(value) }"},
		{"unknown_call", "package main\nfunc Solve(value int) int { return External(value) }"},
		{"pointer", "package main\nfunc Solve(value int) int { p := &value; return *p }"},
		{"channel", "package main\nfunc Solve(value int) int { ch := make(chan int); ch <- value; return <-ch }"},
		{"map", "package main\nfunc Solve(value int) int { values := map[int]int{0:value}; return values[0] }"},
		{"fixed_array", "package main\nfunc Solve(value int) int { values := [1]int{value}; return values[0] }"},
		{"nested_slice", "package main\nfunc Solve(value int) int { values := [][]int{{value}}; return values[0][0] }"},
		{"markdown", "```go\npackage main\nfunc Solve(value int) int { return value }\n```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCompileSpec(CompileSpec{Source: tc.source, FunctionName: "Solve", InputType: "int", OutputType: "int"}); err == nil {
				t.Fatal("受限执行器接受了无效源码")
			}
		})
	}
	for _, name := range []string{"init", "main", "_", "len", "append", "int64"} {
		spec := CompileSpec{Source: "package main\nfunc " + name + "(value int) int { return value }", FunctionName: name, InputType: "int", OutputType: "int"}
		if err := ValidateCompileSpec(spec); err == nil {
			t.Errorf("接受了保留函数名称 %q", name)
		}
	}
	base := "package main\nfunc Solve(value int) int { return value }\n//"
	spec := CompileSpec{Source: base + strings.Repeat(" ", MaxSourceBytes-len(base)), FunctionName: "Solve", InputType: "int", OutputType: "int"}
	if err := ValidateCompileSpec(spec); err != nil {
		t.Fatalf("大小上限内的源码被拒绝: %v", err)
	}
	spec.Source += " "
	if err := ValidateCompileSpec(spec); err == nil {
		t.Fatal("超过大小上限的源码被接受")
	}
}

func TestPublicJSONValidationPreservesTypedValues(t *testing.T) {
	for _, tc := range []struct{ typ, input, want string }{
		{"bool", " true \n", "true"}, {"string", `"你好"`, `"你好"`},
		{"int", "-9223372036854775808", "-9223372036854775808"},
		{"int64", "9223372036854775807", "9223372036854775807"},
		{"float64", "1.50", "1.5"}, {"[]bool", "[true, false]", "[true,false]"},
		{"[]string", `["a", "b"]`, `["a","b"]`}, {"[]int", "[1, -2]", "[1,-2]"},
		{"[]int64", "[9223372036854775807]", "[9223372036854775807]"}, {"[]float64", "[]", "[]"},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			got, err := decodeJSON([]byte(tc.input), tc.typ)
			if err != nil || string(got) != tc.want {
				t.Fatalf("结果 %s，错误 %v；预期 %s", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct{ typ, input string }{
		{"int", "null"}, {"[]int", "null"}, {"[]int", "[1,null]"}, {"[]string", "[1]"},
		{"int", "1.5"}, {"int", "1e0"}, {"int64", "9223372036854775808"},
		{"int64", "-9223372036854775809"}, {"float64", "1e999"}, {"bool", "0"},
		{"string", `"ok" true`}, {"string", `"ok" garbage`}, {"[]int", "[1,"},
		{"[][]int", "[[1]]"}, {"map[string]int", `{"a":1}`},
	} {
		if _, err := decodeJSON([]byte(tc.input), tc.typ); err == nil {
			t.Errorf("接受了类型 %s 的无效输入 %s", tc.typ, tc.input)
		}
	}
	for _, input := range [][]byte{
		{'"', 0xff, '"'},
		[]byte(`"` + strings.Repeat("x", MaxIOBytes) + `"`),
		[]byte(`"` + strings.Repeat("<", MaxIOBytes/6+1) + `"`),
	} {
		if _, err := decodeJSON(input, "string"); err == nil {
			t.Fatal("接受了无效 UTF-8 或超过输出大小上限的 JSON")
		}
	}
}

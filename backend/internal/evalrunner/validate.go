package evalrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

var scalarTypes = map[string]reflect.Type{
	"bool": reflect.TypeOf(false), "string": reflect.TypeOf(""), "int": reflect.TypeOf(int64(0)),
	"int64": reflect.TypeOf(int64(0)), "float64": reflect.TypeOf(float64(0)),
}

func allowedType(name string) bool {
	_, ok := scalarTypes[strings.TrimPrefix(name, "[]")]
	return ok
}
func validateSource(spec CompileSpec) error {
	if len(spec.Source) == 0 || len(spec.Source) > MaxSourceBytes {
		return fmt.Errorf("source must be 1..%d bytes", MaxSourceBytes)
	}
	if !allowedType(spec.InputType) || !allowedType(spec.OutputType) {
		return fmt.Errorf("signature type is not allowlisted")
	}
	if !token.IsIdentifier(spec.FunctionName) || spec.FunctionName == "init" || spec.FunctionName == "main" || spec.FunctionName == "_" {
		return fmt.Errorf("invalid function name")
	}
	for _, name := range []string{"len", "cap", "append", "copy", "make", "panic", "bool", "string", "int", "int64", "float64"} {
		if name == spec.FunctionName {
			return fmt.Errorf("reserved function name")
		}
	}
	f, err := parser.ParseFile(token.NewFileSet(), "solution.go", spec.Source, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return err
	}
	if f.Name.Name != "main" || len(f.Imports) > 0 || len(f.Decls) != 1 {
		return fmt.Errorf("only package main and the specified function are allowed")
	}
	for _, group := range f.Comments {
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(comment.Text, "//"), "/*"))
			if strings.HasPrefix(text, "go:") || strings.HasPrefix(text, "line ") || strings.HasPrefix(text, "+build") {
				return fmt.Errorf("compiler directives are forbidden")
			}
		}
	}
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok || fn.Name.Name != spec.FunctionName || fn.Recv != nil || fn.Type.TypeParams != nil || fn.Body == nil {
		return fmt.Errorf("exactly one concrete named function is required")
	}
	if !matchesField(fn.Type.Params, spec.InputType, true) || !matchesField(fn.Type.Results, spec.OutputType, false) {
		return fmt.Errorf("signature does not match the finite selected types")
	}
	var invalid error
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if invalid != nil {
			return false
		}
		switch n := node.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt, *ast.SelectStmt, *ast.SendStmt, *ast.ChanType, *ast.InterfaceType, *ast.MapType, *ast.StructType, *ast.StarExpr, *ast.SelectorExpr, *ast.TypeAssertExpr, *ast.FuncType:
			invalid = fmt.Errorf("forbidden syntax %T", n)
		case *ast.GenDecl:
			if n.Tok != token.VAR && n.Tok != token.CONST {
				invalid = fmt.Errorf("local type/import declarations are forbidden")
			}
		case *ast.ArrayType:
			typ, ok := n.Elt.(*ast.Ident)
			if n.Len != nil || !ok {
				invalid = fmt.Errorf("only finite slices are allowed")
			} else if _, ok := scalarTypes[typ.Name]; !ok {
				invalid = fmt.Errorf("unknown slice element type")
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND || n.Op == token.MUL || n.Op == token.ARROW {
				invalid = fmt.Errorf("pointer/channel operations forbidden")
			}
		case *ast.CallExpr:
			ident, ok := n.Fun.(*ast.Ident)
			if !ok {
				invalid = fmt.Errorf("indirect calls forbidden")
				break
			}
			if ident.Name == spec.FunctionName {
				break
			}
			switch ident.Name {
			case "len", "cap", "append", "copy", "make", "panic", "bool", "string", "int", "int64", "float64":
			default:
				invalid = fmt.Errorf("call to %s is not allowlisted", ident.Name)
			}
		}
		return invalid == nil
	})
	return invalid
}
func matchesField(fields *ast.FieldList, typ string, argument bool) bool {
	if fields == nil || len(fields.List) != 1 {
		return false
	}
	f := fields.List[0]
	if (argument && len(f.Names) != 1) || len(f.Names) > 1 {
		return false
	}
	node := f.Type
	if strings.HasPrefix(typ, "[]") {
		arr, ok := node.(*ast.ArrayType)
		if !ok || arr.Len != nil {
			return false
		}
		node = arr.Elt
		typ = typ[2:]
	}
	id, ok := node.(*ast.Ident)
	return ok && id.Name == typ
}

// decodeJSON validates one complete value, rejecting null, fractional/overflowing
// integers and wrong scalar/slice types. Go/WASI int is 64 bits. Host re-encoding
// makes whitespace irrelevant to the output contract.
func decodeJSON(data []byte, typ string) ([]byte, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("JSON must be valid UTF-8")
	}
	if len(data) > MaxIOBytes {
		return nil, fmt.Errorf("JSON exceeds %d bytes", MaxIOBytes)
	}
	if !allowedType(typ) {
		return nil, fmt.Errorf("unknown JSON type")
	}
	var shape any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&shape); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing output or incomplete JSON")
	}
	if shape == nil {
		return nil, fmt.Errorf("null is not a scalar/slice result")
	}
	target := scalarTypes[strings.TrimPrefix(typ, "[]")]
	if strings.HasPrefix(typ, "[]") {
		values, ok := shape.([]any)
		if !ok {
			return nil, fmt.Errorf("expected JSON array")
		}
		for _, value := range values {
			if value == nil {
				return nil, fmt.Errorf("null array elements forbidden")
			}
		}
		target = reflect.SliceOf(target)
	}
	value := reflect.New(target)
	if err := json.Unmarshal(data, value.Interface()); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value.Elem().Interface())
	if err != nil {
		return nil, err
	}
	if len(canonical) > MaxIOBytes {
		return nil, fmt.Errorf("canonical JSON exceeds %d bytes", MaxIOBytes)
	}
	return canonical, nil
}

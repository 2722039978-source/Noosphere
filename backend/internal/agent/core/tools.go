package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/noosphere/backend/internal/agent/contract"
	"github.com/noosphere/backend/internal/gateway/providers"
)

type toolArgs struct {
	Index   int     `json:"index"`
	To      string  `json:"to"`
	LogID   string  `json:"log_id"`
	Verify  bool    `json:"verify"`
	Path    string  `json:"path"`
	Offset  int64   `json:"offset"`
	Limit   int     `json:"limit"`
	Query   string  `json:"query"`
	Content *string `json:"content"`
	Hash    string  `json:"expected_hash"`
	Edits   []struct {
		Old string `json:"old"`
		New string `json:"new"`
	} `json:"edits"`
	Cwd     string   `json:"cwd"`
	Program string   `json:"program"`
	Args    []string `json:"args"`
}

// Standard build/test commands are recognized even if an older tool caller
// omitted the optional verification flag.
func verificationKey(call providers.ToolCall) string {
	var a toolArgs
	if json.Unmarshal([]byte(call.Function.Arguments), &a) != nil {
		return ""
	}
	if call.Function.Name == "present" {
		return "交付检查 " + a.Path
	}
	if call.Function.Name != "run_command" {
		return ""
	}
	check := a.Verify
	program := strings.TrimSuffix(strings.ToLower(path.Base(a.Program)), ".exe")
	switch program {
	case "pytest", "ruff", "mypy", "jest", "vitest", "tsc", "eslint":
		check = true
	}
	for _, arg := range a.Args {
		switch arg {
		case "test", "build", "check", "lint", "unittest", "pytest", "--test":
			check = true
		}
	}
	if !check {
		return ""
	}
	command := verificationCommandArgs(a)
	if safeText(command, 400) == command {
		return command
	}
	return fmt.Sprintf("命令校验 %x", sha256.Sum256([]byte(command)))
}

func verificationCommandArgs(a toolArgs) string {
	command, _ := json.Marshal(append([]string{a.Cwd, a.Program}, a.Args...))
	return string(command)
}

func definition(name, description, props string, required ...string) providers.ToolDefinition {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte("{"+props+"}"), &fields); err != nil {
		panic(err)
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "properties": fields, "required": append([]string{}, required...), "additionalProperties": false})
	return providers.ToolDefinition{Type: "function", Function: providers.FunctionDefinition{Name: name, Description: description, Parameters: schema}}
}

const pathField = `"path":{"type":"string"}`
const hashField = `"expected_hash":{"type":"string"}`
const rangeFields = `"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1}`

func (e *engine) definitions() []providers.ToolDefinition {
	defs := []providers.ToolDefinition{
		definition("read_instructions", "Read the complete original goal (index 0) or older steering note by index, in byte ranges.", `"index":{"type":"integer","minimum":0},`+rangeFields, "index"),
		definition("list_files", "List a page of relative paths in the draft. Offset is a file index.", rangeFields),
		definition("search_files", "Find literal text in a bounded page of draft files. Use next_offset to continue.", `"query":{"type":"string"},`+rangeFields, "query"),
		definition("read_file", "Read UTF-8 text by byte range in draft. Binary files return metadata; use next_offset for large files.", pathField+","+rangeFields, "path"),
		definition("write_file", "Create new UTF-8 text or replace text using the SHA-256 from read_file. Omit expected_hash only for creation.", pathField+","+hashField+`,"content":{"type":"string"}`, "path", "content"),
		definition("edit_file", "Apply ordered unique exact-text hunks to a draft file, all or none. Use expected_hash from read_file.", pathField+","+hashField+`,"edits":{"type":"array","items":{"type":"object","properties":{"old":{"type":"string"},"new":{"type":"string"}},"required":["old","new"],"additionalProperties":false}}`, "path", "expected_hash", "edits"),
		definition("delete_file", "Remove a draft file at a verified revision. Publishing a deletion requires approval.", pathField+","+hashField, "path", "expected_hash"),
		definition("move_file", "Move a version-checked draft file to a new relative path. Existing destinations are preserved.", pathField+","+hashField+`,"to":{"type":"string"}`, "path", "to", "expected_hash"),
	}
	if len(e.ConflictedPaths) > 0 {
		defs = append(defs, definition("list_conflicts", "List paths whose user version overlaps draft edits; includes all three revision hashes.", rangeFields),
			definition("read_conflict", "Read a version from a workspace conflict. Read current draft then edit_file/write_file with its current hash to resolve.", pathField+`,"version":{"type":"string","enum":["base","proposal","current"]},`+rangeFields, "path", "version"))
	}
	if e.o.Command != nil {
		defs = append(defs, definition("run_command", "Run an isolated workspace command; pass executable, literal argv and relative cwd. Set verify for acceptance checks. Observe its exit code and fix failures.", `"program":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"cwd":{"type":"string"},"verify":{"type":"boolean"}`, "program", "args"))
	}
	if e.o.ReadLog != nil {
		defs = append(defs, definition("read_command_log", "Read another page of the run's diagnostic output by log_id and byte offset.", `"log_id":{"type":"string"},`+rangeFields, "log_id"))
	}
	if e.o.Generate != nil {
		defs = append(defs, definition("generate_artifact", "Generate an editable PPTX/DOCX, static HTML, PDF report or PNG from title and sections. Path extension selects the format. After generation use present.", pathField+","+hashField+`,"title":{"type":"string"},"sections":{"type":"array","items":{"type":"object","properties":{"heading":{"type":"string"},"paragraphs":{"type":"array","items":{"type":"string"}}},"required":["heading","paragraphs"],"additionalProperties":false}}`, "path", "title", "sections"))
	}
	if e.o.Present != nil {
		defs = append(defs, definition("present", "Validate a finished artifact and prepare its delivery card from this exact file version.", pathField, "path"))
	}
	for _, d := range e.o.Definitions {
		if allowedLookup(d.Function.Name) && e.o.ReadOnly != nil {
			defs = append(defs, d)
		}
	}
	for _, t := range e.o.Protected {
		if e.protected(t.Definition.Function.Name) != nil {
			defs = append(defs, t.Definition)
		}
	}
	return defs
}
func allowedLookup(name string) bool {
	switch name {
	case "project_context", "project_trace", "system_processes", "system_connections", "system_services", "service_status":
		return true
	}
	return false
}
func inspectionTool(name string) bool {
	switch name {
	case "list_files", "search_files", "read_file", "read_command_log", "service_status", "system_services":
		return true
	}
	return false
}
func (e *engine) dispatch(ctx context.Context, call providers.ToolCall) (any, error) {
	name, raw := call.Function.Name, json.RawMessage(call.Function.Arguments)
	if name == "read_conflict" || name == "list_conflicts" {
		if len(e.ConflictedPaths) == 0 {
			return nil, errors.New("当前工作区没有冲突")
		}
		return e.conflictTool(ctx, call)
	}
	if name == "generate_artifact" {
		if e.o.Generate == nil || len(raw) > 32000 {
			return nil, errors.New("文档工具包不可用或请求超出限制")
		}
		var a toolArgs
		_ = json.Unmarshal(raw, &a)
		_, before, _ := e.d.Stat(ctx, a.Path)
		out, err := e.o.Generate(ctx, raw)
		if err == nil {
			_, after, statErr := e.d.Stat(ctx, a.Path)
			e.mutatedFile = statErr != nil || before != after
		}
		return out, err
	}
	if allowedLookup(name) && e.o.ReadOnly != nil {
		for _, d := range e.o.Definitions {
			if d.Function.Name == name {
				return e.o.ReadOnly(ctx, name, raw)
			}
		}
	}
	if len(raw) > 32000 {
		return nil, errors.New("工具参数超出传输限制；请分段编辑或使用草稿脚本")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var a toolArgs
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("工具参数无效: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("工具参数有多余内容")
	}
	switch name {
	case "read_instructions":
		if a.Index < 0 || a.Index > len(e.Notes) || a.Offset < 0 || a.Limit < 0 {
			return nil, errors.New("指令索引或分页范围无效")
		}
		text := e.goal
		if a.Index > 0 {
			text = e.Notes[a.Index-1]
		}
		start := min(a.Offset, int64(len(text)))
		if start < int64(len(text)) && !utf8.RuneStart(text[start]) {
			return nil, errors.New("分页位置需要位于 UTF-8 字符边界")
		}
		end := min(int64(len(text)), start+int64(min(max(4, positive(a.Limit, 4096)), 10000)))
		for end > start && end < int64(len(text)) && !utf8.RuneStart(text[end]) {
			end--
		}
		return map[string]any{"index": a.Index, "text": safeText(text[start:end], 10000), "next_offset": end, "done": end == int64(len(text))}, nil
	case "list_files", "search_files":
		return e.inventory(ctx, name, a)
	case "read_file":
		return e.read(ctx, a)
	case "write_file":
		if a.Content == nil || strings.ContainsRune(*a.Content, 0) || !utf8.ValidString(*a.Content) {
			return nil, errors.New("请输入 UTF-8 文本；二进制请由工作区运行器生成")
		}
		if err := e.d.Replace(ctx, a.Path, a.Hash, []byte(*a.Content)); err != nil {
			return nil, err
		}
		if a.Hash != fmt.Sprintf("%x", sha256.Sum256([]byte(*a.Content))) {
			e.Recent = nil
			e.mutatedFile = true
		}
		return map[string]any{"ok": true, "path": a.Path}, nil
	case "edit_file":
		return e.edit(ctx, a)
	case "delete_file":
		_, hash, err := e.d.Stat(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		if a.Hash == "" || hash != a.Hash {
			return nil, errors.New("草稿文件版本已变化，请重新读取")
		}
		if err = e.d.Delete(ctx, a.Path); err != nil {
			return nil, err
		}
		e.Recent = nil
		e.mutatedFile = true
		return map[string]any{"ok": true, "path": a.Path}, nil
	case "move_file":
		if a.Hash == "" {
			return nil, errors.New("移动需要提供源文件 SHA-256")
		}
		if err := e.d.Move(ctx, a.Path, a.To, a.Hash); err != nil {
			return nil, err
		}
		e.Recent = nil
		e.mutatedFile = true
		return map[string]any{"ok": true, "from": a.Path, "to": a.To}, nil
	case "read_command_log":
		if e.o.ReadLog == nil {
			break
		}
		if a.LogID == "" || a.Offset < 0 || a.Limit < 0 {
			return nil, errors.New("日志 ID 或读取范围无效")
		}
		content, err := e.o.ReadLog(ctx, a.LogID, a.Offset, min(positive(a.Limit, 4096), 10000))
		if err != nil {
			return nil, err
		}
		return map[string]any{"log_id": a.LogID, "offset": a.Offset, "next_offset": a.Offset + int64(len(content)), "text": safeText(content, 10000)}, nil
	case "run_command":
		if e.o.Command == nil {
			break
		}
		if a.Program == "" || len(a.Args) > 128 {
			return nil, errors.New("请输入程序和有效 argv")
		}
		result, err := e.o.Command(ctx, a.Cwd, a.Program, a.Args)
		result.Output = safeText(result.Output, 10000)
		return result, err
	case "present":
		if e.o.Present == nil {
			break
		}
		_, hash, err := e.d.Stat(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		metadata, err := e.o.Present(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		e.Artifacts = append(removeArtifact(e.Artifacts, a.Path), artifact{Path: a.Path, Hash: hash, Metadata: encoded})
		if e.o.PreviewImage != nil {
			e.PendingPreview = a.Path
		}
		return metadata, nil
	}
	return nil, fmt.Errorf("此宿主未授权工具 %q", safeText(name, 80))
}

func (e *engine) runDraftCommand(ctx context.Context, call providers.ToolCall) (any, string, bool, error) {
	before := ""
	if len(e.Checked) > 0 {
		changes, err := e.d.Changes(ctx)
		if err != nil {
			return nil, "", false, fmt.Errorf("命令执行前的草稿检查失败: %w", err)
		}
		before = contract.ManifestDigest(changes)
	}
	out, err := e.dispatch(ctx, call)
	if err != nil && before == "" {
		return out, "", false, err
	}
	changes, scanErr := e.d.Changes(ctx)
	if scanErr != nil {
		if err == nil {
			err = fmt.Errorf("命令结束后的草稿检查失败: %w", scanErr)
		}
		return out, "", before != "", err
	}
	after := contract.ManifestDigest(changes)
	return out, after, before != "" && before != after, err
}

func (e *engine) inventory(ctx context.Context, name string, a toolArgs) (any, error) {
	if a.Offset < 0 || a.Limit < 0 || (name == "search_files" && (a.Query == "" || len(a.Query) > 1000)) {
		return nil, errors.New("查询或分页参数无效")
	}
	files, err := e.d.List(ctx)
	if err != nil {
		return nil, err
	}
	start := min(int64(len(files)), a.Offset)
	end := min(int64(len(files)), start+int64(min(positive(a.Limit, 80), 200)))
	if name == "list_files" {
		return map[string]any{"paths": files[start:end], "next_offset": end, "done": end == int64(len(files))}, nil
	}
	matches := []map[string]string{}
	for _, path := range files[start:end] {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		// This search examines bounded prefixes; the output makes the scope
		// explicit, and read_file can page the rest of any specific file.
		chunk, readErr := e.d.Read(ctx, path, 0, 32768)
		if readErr != nil || bytes.IndexByte(chunk, 0) >= 0 || !utf8.Valid(chunk) {
			continue
		}
		if i := bytes.Index(chunk, []byte(a.Query)); i >= 0 {
			matches = append(matches, map[string]string{"path": path, "excerpt": safeText(string(chunk[i:]), 160)})
		}
	}
	return map[string]any{"matches": matches, "next_offset": end, "done": end == int64(len(files)), "bytes_per_file": 32768}, nil
}
func (e *engine) read(ctx context.Context, a toolArgs) (any, error) {
	if a.Offset < 0 || a.Limit < 0 {
		return nil, errors.New("读取范围无效")
	}
	size, hash, err := e.d.Stat(ctx, a.Path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"path": a.Path, "size": size, "sha256": hash}
	if a.Offset >= size {
		out["done"] = true
		return out, nil
	}
	limit := min(positive(a.Limit, 4096), 10000)
	// Read a few extra bytes to avoid splitting a UTF-8 rune between pages.
	chunk, err := e.d.Read(ctx, a.Path, a.Offset, limit+utf8.UTFMax)
	if err != nil {
		return nil, err
	}
	end := min(limit, len(chunk))
	if end < len(chunk) {
		for end > 0 && !utf8.RuneStart(chunk[end]) {
			end--
		}
	}
	if end == 0 && len(chunk) > 0 {
		_, end = utf8.DecodeRune(chunk)
	}
	chunk = chunk[:end]
	if bytes.IndexByte(chunk, 0) >= 0 || !utf8.Valid(chunk) {
		out["binary_or_invalid_offset"] = true
		return out, nil
	}
	out["text"] = safeText(string(chunk), 10000)
	out["next_offset"] = a.Offset + int64(end)
	out["done"] = a.Offset+int64(end) >= size
	return out, nil
}
func (e *engine) edit(ctx context.Context, a toolArgs) (any, error) {
	if a.Hash == "" || len(a.Edits) == 0 || len(a.Edits) > 100 {
		return nil, errors.New("请提供文件版本及 1–100 处编辑")
	}
	size, hash, err := e.d.Stat(ctx, a.Path)
	if err != nil {
		return nil, err
	}
	if hash != a.Hash {
		return nil, errors.New("当前版本与 expected_hash 不符")
	}
	if size > 8<<20 {
		return nil, errors.New("大于 8 MiB 的文件请用隔离运行器流式修改")
	}
	// The host verifies a file's full revision as it reads. Read this bounded
	// edit target once so multi-hunk editing scales linearly with file size.
	data, err := e.d.Read(ctx, a.Path, 0, int(size))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, io.ErrUnexpectedEOF
	}
	text := string(data)
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return nil, errors.New("多处编辑需要 UTF-8 文本")
	}
	for i, h := range a.Edits {
		if h.Old == "" || strings.Count(text, h.Old) != 1 || strings.ContainsRune(h.New, 0) {
			return nil, fmt.Errorf("第 %d 处编辑无法唯一匹配，本次编辑尚未写入", i+1)
		}
		text = strings.Replace(text, h.Old, h.New, 1)
	}
	if text == string(data) {
		return nil, errors.New("编辑没有改变文件内容")
	}
	if err = e.d.Replace(ctx, a.Path, a.Hash, []byte(text)); err != nil {
		return nil, err
	}
	e.Recent = nil
	e.mutatedFile = true
	return map[string]any{"ok": true, "path": a.Path, "edits": len(a.Edits)}, nil
}

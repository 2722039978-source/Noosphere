package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/noosphere/backend/internal/agent/contract"
	"github.com/noosphere/backend/internal/gateway/providers"
)

type toolArgs struct {
	Index     int     `json:"index"`
	To        string  `json:"to"`
	LogID     string  `json:"log_id"`
	Verify    bool    `json:"verify"`
	Path      string  `json:"path"`
	Offset    int64   `json:"offset"`
	Limit     int     `json:"limit"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Query     string  `json:"query"`
	Content   *string `json:"content"`
	Hash      string  `json:"expected_hash"`
	Edits     []struct {
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
const lineRangeFields = `"start_line":{"type":"integer","minimum":1},"end_line":{"type":"integer","minimum":1}`

func (e *engine) definitions() []providers.ToolDefinition {
	defs := []providers.ToolDefinition{
		definition("read_instructions", "Read the complete original goal (index 0) or older steering note by index, in byte ranges.", `"index":{"type":"integer","minimum":0},`+rangeFields, "index"),
		definition("list_files", "List a page of relative paths in the draft. Offset is a file index.", rangeFields),
		definition("search_files", "Find literal text in a bounded page of draft files with line numbers. Use next_offset to continue.", `"query":{"type":"string"},`+rangeFields, "query"),
		definition("read_file", "Read UTF-8 text in draft by line range (start_line/end_line, 1-based) or byte range (offset/limit). Records file revision for later edits.", pathField+","+lineRangeFields+","+rangeFields, "path"),
		definition("write_file", "Create new UTF-8 text or replace an already-read file. expected_hash is optional after read_file in this run (short prefix or full SHA-256 also accepted).", pathField+","+hashField+`,"content":{"type":"string"}`, "path", "content"),
		definition("edit_file", "Apply ordered unique exact-text hunks to an already-read draft file, all or none. expected_hash is optional after read_file (short prefix or full SHA-256 also accepted).", pathField+","+hashField+`,"edits":{"type":"array","items":{"type":"object","properties":{"old":{"type":"string"},"new":{"type":"string"}},"required":["old","new"],"additionalProperties":false}}`, "path", "edits"),
		definition("delete_file", "Remove a verified draft file. expected_hash is optional after read_file (short prefix or full SHA-256 also accepted). Publishing a deletion requires approval.", pathField+","+hashField, "path"),
		definition("move_file", "Move a verified draft file to a new relative path. expected_hash is optional after read_file. Existing destinations are preserved.", pathField+","+hashField+`,"to":{"type":"string"}`, "path", "to"),
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
		defs = append(defs, definition("generate_artifact", "Generate a rich editable PPTX/DOCX, static HTML, PDF report or PNG. Supports themes, layouts (bullets/two_column/cards/metrics/table/quote), bullet lists, columns, KPI metrics, native tables, embedded workspace images, and speaker notes. Path extension selects format. After generation use present.", pathField+","+hashField+`,"title":{"type":"string"},"subtitle":{"type":"string"},"author":{"type":"string"},"theme":{"type":"string","enum":["noosphere","slate","warm","dark"]},"sections":{"type":"array","items":{"type":"object","properties":{"heading":{"type":"string"},"subtitle":{"type":"string"},"layout":{"type":"string","enum":["","bullets","two_column","cards","metrics","table","quote"]},"paragraphs":{"type":"array","items":{"type":"string"}},"bullets":{"type":"array","items":{"type":"string"}},"columns":{"type":"array","items":{"type":"object","properties":{"heading":{"type":"string"},"paragraphs":{"type":"array","items":{"type":"string"}},"bullets":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}},"metrics":{"type":"array","items":{"type":"object","properties":{"label":{"type":"string"},"value":{"type":"string"},"note":{"type":"string"}},"required":["label","value"],"additionalProperties":false}},"table":{"type":"object","properties":{"headers":{"type":"array","items":{"type":"string"}},"rows":{"type":"array","items":{"type":"array","items":{"type":"string"}}}},"required":["headers","rows"],"additionalProperties":false},"image_path":{"type":"string"},"image_alt":{"type":"string"},"notes":{"type":"string"}},"required":["heading"],"additionalProperties":false}}`, "path", "title", "sections"))
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
		if e.o.Generate == nil || len(raw) > 128000 {
			return nil, errors.New("文档工具包不可用或请求超出限制")
		}
		var a toolArgs
		_ = json.Unmarshal(raw, &a)
		if a.Path != "" {
			resolved, err := e.resolveExpectedHash(ctx, a.Path, a.Hash, true)
			if err != nil {
				return nil, err
			}
			if resolved != a.Hash {
				var payload map[string]any
				if json.Unmarshal(raw, &payload) == nil && payload != nil {
					payload["expected_hash"] = resolved
					if patched, err := json.Marshal(payload); err == nil {
						raw = patched
					}
				}
			}
		}
		_, before, _ := e.d.Stat(ctx, a.Path)
		out, err := e.o.Generate(ctx, raw)
		if err == nil {
			_, after, statErr := e.d.Stat(ctx, a.Path)
			e.mutatedFile = statErr != nil || before != after
			if statErr == nil && after != "" {
				e.observeHash(a.Path, after)
			}
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
		expected, err := e.resolveExpectedHash(ctx, a.Path, a.Hash, true)
		if err != nil {
			return nil, err
		}
		if err := e.d.Replace(ctx, a.Path, expected, []byte(*a.Content)); err != nil {
			return nil, err
		}
		newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(*a.Content)))
		e.observeHash(a.Path, newHash)
		if expected != newHash {
			e.Recent = nil
			e.mutatedFile = true
		}
		return map[string]any{"ok": true, "path": a.Path, "sha256": newHash, "short_hash": shortHash(newHash)}, nil
	case "edit_file":
		return e.edit(ctx, a)
	case "delete_file":
		expected, err := e.resolveExpectedHash(ctx, a.Path, a.Hash, false)
		if err != nil {
			return nil, err
		}
		_, hash, err := e.d.Stat(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		if expected == "" || hash != expected {
			return nil, errors.New("草稿文件版本已变化，请重新读取")
		}
		if err = e.d.Delete(ctx, a.Path); err != nil {
			return nil, err
		}
		e.forgetHash(a.Path)
		e.Recent = nil
		e.mutatedFile = true
		return map[string]any{"ok": true, "path": a.Path}, nil
	case "move_file":
		expected, err := e.resolveExpectedHash(ctx, a.Path, a.Hash, false)
		if err != nil {
			return nil, err
		}
		if expected == "" {
			return nil, errors.New("移动前请先读取源文件或提供 expected_hash")
		}
		if err := e.d.Move(ctx, a.Path, a.To, expected); err != nil {
			return nil, err
		}
		e.forgetHash(a.Path)
		e.observeHash(a.To, expected)
		e.Recent = nil
		e.mutatedFile = true
		return map[string]any{"ok": true, "from": a.Path, "to": a.To, "sha256": expected, "short_hash": shortHash(expected)}, nil
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
	matches := []map[string]any{}
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
			line := bytes.Count(chunk[:i], []byte{'\n'}) + 1
			matches = append(matches, map[string]any{"path": path, "line": line, "excerpt": safeText(string(chunk[i:]), 160)})
		}
	}
	return map[string]any{"matches": matches, "next_offset": end, "done": end == int64(len(files)), "bytes_per_file": 32768}, nil
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func isHexPrefix(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	if len(s)%2 == 1 {
		s += "0"
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func (e *engine) observeHash(filePath, hash string) {
	if filePath == "" || hash == "" {
		return
	}
	if e.ObservedHashes == nil {
		e.ObservedHashes = map[string]string{}
	}
	e.ObservedHashes[filePath] = hash
}

func (e *engine) forgetHash(filePath string) {
	if e.ObservedHashes != nil {
		delete(e.ObservedHashes, filePath)
	}
}

func (e *engine) resolveExpectedHash(ctx context.Context, filePath, provided string, allowCreate bool) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided != "" {
		if len(provided) == 64 {
			return provided, nil
		}
		if !isHexPrefix(provided) {
			return "", errors.New("当前版本与 expected_hash 不符")
		}
		_, current, err := e.d.Stat(ctx, filePath)
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(strings.ToLower(current), strings.ToLower(provided)) {
			return "", errors.New("当前版本与 expected_hash 不符，请重新读取文件")
		}
		return current, nil
	}
	_, current, err := e.d.Stat(ctx, filePath)
	if errors.Is(err, fs.ErrNotExist) {
		if allowCreate {
			return "", nil
		}
		return "", err
	}
	if err != nil {
		return "", err
	}
	observed := e.ObservedHashes[filePath]
	if observed == "" {
		return "", errors.New("修改已有文件前请先调用 read_file 读取，或提供 expected_hash")
	}
	if observed != current {
		return "", errors.New("草稿文件自上次读取后已变化，请先重新调用 read_file")
	}
	return current, nil
}

func (e *engine) read(ctx context.Context, a toolArgs) (any, error) {
	if a.Offset < 0 || a.Limit < 0 || a.StartLine < 0 || a.EndLine < 0 || (a.EndLine > 0 && a.StartLine > a.EndLine) {
		return nil, errors.New("读取范围无效")
	}
	size, hash, err := e.d.Stat(ctx, a.Path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"path": a.Path, "size": size, "sha256": hash, "short_hash": shortHash(hash)}
	if a.StartLine > 0 || a.EndLine > 0 {
		if size > 8<<20 {
			return nil, errors.New("大于 8 MiB 的文件请使用 offset/limit 字节分页读取")
		}
		data, err := e.d.Read(ctx, a.Path, 0, int(size))
		if err != nil {
			return nil, err
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			out["binary_or_invalid_offset"] = true
			return out, nil
		}
		e.observeHash(a.Path, hash)
		lines := strings.Split(string(data), "\n")
		total := len(lines)
		start := max(1, a.StartLine)
		out["total_lines"] = total
		if start > total {
			out["start_line"] = start
			out["end_line"] = total
			out["done"] = true
			out["text"] = ""
			return out, nil
		}
		requestedEnd := a.EndLine
		if requestedEnd == 0 {
			requestedEnd = start + 199
		}
		requestedEnd = min(total, min(requestedEnd, start+299))
		var b strings.Builder
		actualEnd := start - 1
		for i := start; i <= requestedEnd; i++ {
			line := lines[i-1]
			addLen := len(line)
			if actualEnd >= start {
				addLen++
			}
			if actualEnd >= start && b.Len()+addLen > 10000 {
				break
			}
			if actualEnd >= start {
				b.WriteByte('\n')
			}
			b.WriteString(line)
			actualEnd = i
			if b.Len() >= 10000 {
				break
			}
		}
		out["start_line"] = start
		out["end_line"] = actualEnd
		out["done"] = actualEnd >= total
		if actualEnd < total {
			out["next_line"] = actualEnd + 1
		}
		out["text"] = safeText(b.String(), 10000)
		return out, nil
	}
	if a.Offset >= size {
		e.observeHash(a.Path, hash)
		out["done"] = true
		return out, nil
	}
	limit := min(positive(a.Limit, 4096), 10000)
	// Read up to UTFMax bytes before a non-zero offset so a mid-character offset
	// aligns gently to the containing UTF-8 rune instead of failing on CJK text.
	readOffset := a.Offset
	lead := int64(0)
	if a.Offset > 0 {
		lead = min(int64(utf8.UTFMax-1), a.Offset)
		readOffset = a.Offset - lead
	}
	chunk, err := e.d.Read(ctx, a.Path, readOffset, int(lead)+limit+utf8.UTFMax)
	if err != nil {
		return nil, err
	}
	startIdx := int(lead)
	if startIdx < len(chunk) && !utf8.RuneStart(chunk[startIdx]) {
		for startIdx > 0 && !utf8.RuneStart(chunk[startIdx]) {
			startIdx--
		}
	}
	alignedOffset := readOffset + int64(startIdx)
	chunk = chunk[startIdx:]
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
	e.observeHash(a.Path, hash)
	if alignedOffset != a.Offset {
		out["aligned_offset"] = alignedOffset
	}
	out["text"] = safeText(string(chunk), 10000)
	out["next_offset"] = alignedOffset + int64(end)
	out["done"] = alignedOffset+int64(end) >= size
	return out, nil
}
func (e *engine) edit(ctx context.Context, a toolArgs) (any, error) {
	if len(a.Edits) == 0 || len(a.Edits) > 100 {
		return nil, errors.New("请提供 1–100 处编辑")
	}
	expected, err := e.resolveExpectedHash(ctx, a.Path, a.Hash, false)
	if err != nil {
		return nil, err
	}
	size, hash, err := e.d.Stat(ctx, a.Path)
	if err != nil {
		return nil, err
	}
	if hash != expected {
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
	if err = e.d.Replace(ctx, a.Path, expected, []byte(text)); err != nil {
		return nil, err
	}
	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	e.observeHash(a.Path, newHash)
	e.Recent = nil
	e.mutatedFile = true
	return map[string]any{"ok": true, "path": a.Path, "edits": len(a.Edits), "sha256": newHash, "short_hash": shortHash(newHash)}, nil
}

package codelens

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	appErrors "github.com/noosphere/backend/internal/errors"
	"github.com/noosphere/backend/internal/workspaces"
)

const (
	reaVersion       = "6.8.0"
	maxArtifactBytes = 64 << 20
	maxEvidenceBytes = 8 << 20
)

var evidenceDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ArtifactEvidence struct {
	ID         string          `json:"id"`
	Tool       string          `json:"tool"`
	Path       string          `json:"path"`
	SourceHash string          `json:"source_hash"`
	ObservedAt string          `json:"observed_at"`
	Evidence   json.RawMessage `json:"evidence"`
}

type ArtifactAnalyzer struct {
	workspaces  *workspaces.Manager
	storage     string
	runtimeRoot string
	slot        chan struct{}
}

func NewArtifactAnalyzer(manager *workspaces.Manager, dataDir, runtimeRoot string) *ArtifactAnalyzer {
	return &ArtifactAnalyzer{
		workspaces: manager, storage: filepath.Join(dataDir, "codelens-artifacts"),
		runtimeRoot: runtimeRoot, slot: make(chan struct{}, 1),
	}
}

func (a *ArtifactAnalyzer) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/codelens/artifacts", appErrors.WrapHandler(a.inspect))
	mux.HandleFunc("GET /api/v1/codelens/artifacts/{id}", appErrors.WrapHandler(a.get))
}

func (a *ArtifactAnalyzer) workspace(id string) (workspaces.Workspace, *appErrors.AppError) {
	workspace, err := a.workspaces.Workspace(id)
	if errors.Is(err, workspaces.ErrNotFound) {
		return workspace, appErrors.NotFound(appErrors.CatCodeLens, "工作区不存在")
	}
	if err != nil {
		return workspace, appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法读取工作区")
	}
	if workspace.ProjectPath == "" {
		return workspace, appErrors.InvalidInput(appErrors.CatCodeLens, "请先为工作区打开项目目录")
	}
	return workspace, nil
}

func (a *ArtifactAnalyzer) inspect(w http.ResponseWriter, r *http.Request) *appErrors.AppError {
	var request struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
		Tool        string `json:"tool"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(new(any)) != io.EOF {
		return appErrors.InvalidInput(appErrors.CatCodeLens, "产物分析请求格式无效")
	}
	if request.Tool != "inspect-pe-imports" && request.Tool != "inspect-go-binary" {
		return appErrors.InvalidInput(appErrors.CatCodeLens, "不支持此产物分析方式")
	}
	name := strings.ReplaceAll(request.Path, "\\", "/")
	if len(name) > 1024 || name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, ":*?\"<>|\x00") || !filepath.IsLocal(filepath.FromSlash(name)) {
		return appErrors.InvalidInput(appErrors.CatCodeLens, "请指定项目目录内的相对文件路径")
	}
	workspace, problem := a.workspace(request.WorkspaceID)
	if problem != nil {
		return problem
	}
	select {
	case a.slot <- struct{}{}:
		defer func() { <-a.slot }()
	default:
		return appErrors.New(appErrors.ErrConflict, appErrors.CatCodeLens, "已有产物正在分析")
	}
	if !a.ready() {
		return appErrors.New(appErrors.ErrUnavailable, appErrors.CatCodeLens, "REA 运行组件不可用，请使用完整的 Noosphere 分发包")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	work := filepath.Join(a.storage, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		return appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法创建产物分析目录")
	}
	directory, err := os.MkdirTemp(work, "analysis-")
	if err != nil {
		return appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法创建产物分析目录")
	}
	snapshot := filepath.Join(directory, filepath.Base(name))
	defer os.Remove(directory)
	defer os.Remove(snapshot)
	digest, err := copyArtifact(ctx, workspace.ProjectPath, name, snapshot)
	if err != nil {
		return appErrors.Wrap(err, appErrors.ErrInvalidInput, appErrors.CatCodeLens, "无法读取项目产物，请选择 64 MiB 以内的普通文件")
	}
	current, err := a.workspaces.Workspace(workspace.ID)
	if err != nil || current.ProjectPath != workspace.ProjectPath {
		return appErrors.New(appErrors.ErrConflict, appErrors.CatCodeLens, "分析期间工作区的项目目录已经变化")
	}
	store := a.workspaceStorage(workspace)
	id := artifactID(request.Tool, name, digest)
	file := filepath.Join(store, id+".json")
	saved, err := readArtifactEvidence(file, id)
	if err == nil {
		writeArtifact(w, http.StatusCreated, saved)
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法读取已保存的产物证据")
	}
	result, err := a.run(ctx, request.Tool, snapshot, digest)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return appErrors.New(appErrors.ErrTimeout, appErrors.CatCodeLens, "REA 产物分析超时")
		}
		return appErrors.Wrap(err, appErrors.ErrInvalidInput, appErrors.CatCodeLens, "REA 无法用所选方式分析此文件")
	}
	current, err = a.workspaces.Workspace(workspace.ID)
	if err != nil || current.ProjectPath != workspace.ProjectPath {
		return appErrors.New(appErrors.ErrConflict, appErrors.CatCodeLens, "分析期间工作区的项目目录已经变化")
	}
	saved = ArtifactEvidence{
		ID: id, Tool: request.Tool, Path: name, SourceHash: digest,
		ObservedAt: time.Now().UTC().Format(time.RFC3339), Evidence: result,
	}
	if err := saveArtifactEvidence(store, file, saved); err != nil {
		return appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法保存产物证据")
	}
	writeArtifact(w, http.StatusCreated, saved)
	return nil
}

func (a *ArtifactAnalyzer) get(w http.ResponseWriter, r *http.Request) *appErrors.AppError {
	id := r.PathValue("id")
	if !evidenceDigest.MatchString(id) {
		return appErrors.NotFound(appErrors.CatCodeLens, "产物证据不存在")
	}
	workspace, problem := a.workspace(r.URL.Query().Get("workspace_id"))
	if problem != nil {
		return problem
	}
	evidence, err := readArtifactEvidence(filepath.Join(a.workspaceStorage(workspace), id+".json"), id)
	if errors.Is(err, os.ErrNotExist) {
		return appErrors.NotFound(appErrors.CatCodeLens, "产物证据不存在")
	}
	if err != nil {
		return appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法读取产物证据")
	}
	writeArtifact(w, http.StatusOK, evidence)
	return nil
}

func (a *ArtifactAnalyzer) workspaceStorage(workspace workspaces.Workspace) string {
	key := sha256.Sum256([]byte(workspace.ID + "\x00" + workspace.ProjectPath))
	return filepath.Join(a.storage, fmt.Sprintf("%x", key))
}

func artifactID(tool, name, digest string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(reaVersion+"\x00"+tool+"\x00"+name+"\x00"+digest)))
}

func copyArtifact(ctx context.Context, projectPath, name, destination string) (string, error) {
	root, err := os.OpenRoot(projectPath)
	if err != nil {
		return "", err
	}
	defer root.Close()
	parts := strings.Split(name, "/")
	for i := range parts {
		entry, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if err != nil {
			return "", err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("产物路径不能包含符号链接")
		}
	}
	initial, err := root.Stat(filepath.FromSlash(name))
	if err != nil || !initial.Mode().IsRegular() || initial.Size() > maxArtifactBytes {
		return "", fmt.Errorf("产物必须是 64 MiB 以内的普通文件")
	}
	input, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return "", err
	}
	defer input.Close()
	before, err := input.Stat()
	if err != nil || !os.SameFile(initial, before) || initial.ModTime() != before.ModTime() || initial.Size() != before.Size() {
		return "", fmt.Errorf("读取期间产物已经变化")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(output, digest), io.LimitReader(input, maxArtifactBytes+1))
	closeErr := output.Close()
	if err != nil || closeErr != nil {
		return "", errors.Join(err, closeErr)
	}
	after, err := input.Stat()
	if err != nil || written != before.Size() || after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		return "", fmt.Errorf("读取期间产物已经变化")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func (a *ArtifactAnalyzer) ready() bool {
	if !filepath.IsAbs(a.runtimeRoot) {
		return false
	}
	manifestPath := filepath.Join(a.runtimeRoot, "node_modules", "rea-agents", "package.json")
	manifest, err := os.ReadFile(manifestPath)
	var version struct {
		Version string `json:"version"`
	}
	return err == nil && json.Unmarshal(manifest, &version) == nil && version.Version == reaVersion
}

func (a *ArtifactAnalyzer) run(ctx context.Context, tool, snapshot, digest string) (json.RawMessage, error) {
	node := filepath.Join(a.runtimeRoot, "node.exe")
	script := filepath.Join(a.runtimeRoot, "node_modules", "rea-agents", "scripts", "rea.mjs")
	command := exec.CommandContext(ctx, node, script, tool, snapshot, "--json")
	configureWorker(command)
	command.Dir = a.runtimeRoot
	command.WaitDelay = 2 * time.Second
	command.Env = []string{"TEMP=" + filepath.Dir(snapshot), "TMP=" + filepath.Dir(snapshot)}
	for _, key := range []string{"SystemRoot", "WINDIR", "USERPROFILE", "LOCALAPPDATA", "APPDATA"} {
		if value, ok := os.LookupEnv(key); ok {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	var output evidenceBuffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return nil, err
	}
	var result struct {
		Operation string `json:"operation"`
		ID        string `json:"evidence_id"`
		Subject   struct {
			Digest struct {
				Hash string `json:"sha256"`
			} `json:"digest"`
		} `json:"subject"`
		Normalized json.RawMessage `json:"normalized_result"`
	}
	if err := json.Unmarshal(output.buffer.Bytes(), &result); err != nil {
		return nil, err
	}
	if result.Operation != strings.ReplaceAll(tool, "-", "_") || result.ID == "" || result.Subject.Digest.Hash != digest || len(result.Normalized) == 0 || result.Normalized[0] != '{' {
		return nil, fmt.Errorf("REA 证据与所选文件不匹配")
	}
	return output.buffer.Bytes(), nil
}

type evidenceBuffer struct{ buffer bytes.Buffer }

func (b *evidenceBuffer) Write(p []byte) (int, error) {
	if len(p) > maxEvidenceBytes-b.buffer.Len() {
		return 0, fmt.Errorf("REA 证据超过大小限制")
	}
	return b.buffer.Write(p)
}

func readArtifactEvidence(file, id string) (ArtifactEvidence, error) {
	var evidence ArtifactEvidence
	input, err := os.Open(file)
	if err != nil {
		return evidence, err
	}
	defer input.Close()
	if err := json.NewDecoder(io.LimitReader(input, maxEvidenceBytes+4096)).Decode(&evidence); err != nil {
		return evidence, err
	}
	if evidence.ID != id || !evidenceDigest.MatchString(evidence.SourceHash) || len(evidence.Evidence) == 0 || evidence.Evidence[0] != '{' {
		return evidence, fmt.Errorf("产物证据标识无效")
	}
	return evidence, nil
}

func saveArtifactEvidence(directory, file string, evidence ArtifactEvidence) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	output, err := os.CreateTemp(directory, "evidence-")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(evidence); err != nil {
		return errors.Join(err, output.Close())
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Rename(output.Name(), file)
}

func writeArtifact(w http.ResponseWriter, status int, evidence ArtifactEvidence) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(evidence)
}

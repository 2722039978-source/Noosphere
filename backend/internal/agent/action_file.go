package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

type fileTarget struct {
	parent      *os.Root
	text        string
	exists      bool
	mode        os.FileMode
	directoryID string
	fileID      string
}

var errFileSpellingChanged = errors.New("文件名的大小写或 Unicode 拼写已变化，请使用项目中的确切名称")

// A case-insensitive filesystem can resolve a moved or differently spelled
// directory entry to the same file identity. Use the stored directory name so
// a reviewed replacement does not silently rename that entry.
func requireFileSpelling(directory *os.File, name string) error {
	for {
		names, err := directory.Readdirnames(128)
		for _, actual := range names {
			if actual == name {
				return nil
			}
		}
		if err == io.EOF {
			return errFileSpellingChanged
		}
		if err != nil {
			return err
		}
	}
}

func inspectFile(rootPath, name string) (target fileTarget, err error) {
	if !allowedPath(name) || !safeProjectComponent(path.Base(name)) {
		return target, fmt.Errorf("不允许操作此项目路径")
	}
	root, err := openProjectRoot(rootPath)
	if err != nil {
		return target, err
	}
	defer root.Close()
	rootFile, err := root.Open(".")
	if err != nil {
		return target, err
	}
	defer rootFile.Close()
	rootID, err := directoryIdentity(rootFile)
	if err != nil {
		return target, err
	}
	directory, err := checkedDirectory(root, path.Dir(name))
	if err != nil {
		return target, err
	}
	defer directory.Close()
	parent, err := root.OpenRoot(path.Dir(name))
	if err != nil {
		return target, err
	}
	defer func() {
		if err != nil {
			parent.Close()
		}
	}()
	checked, err := directory.Stat()
	if err != nil {
		return target, err
	}
	opened, err := parent.Stat(".")
	if err != nil || !os.SameFile(checked, opened) {
		return target, fmt.Errorf("文件父目录已变化")
	}
	dirID, err := directoryIdentity(directory)
	if err != nil {
		return target, err
	}
	target.parent = parent
	target.directoryID = rootID + "/" + dirID

	base := path.Base(name)
	entry, err := parent.Lstat(base)
	if os.IsNotExist(err) {
		target.mode = 0600
		return target, nil
	}
	if err != nil || !entry.Mode().IsRegular() || entry.Size() > 64*1024 {
		return target, fmt.Errorf("仅支持不超过 64 KiB 的普通文本文件")
	}
	if err = requireFileSpelling(directory, base); err != nil {
		return target, err
	}
	f, err := parent.OpenFile(base, os.O_RDONLY|fileReadFlags, 0)
	if err != nil {
		return target, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(entry, info) || entry.ModTime() != info.ModTime() || entry.Size() != info.Size() {
		return target, fmt.Errorf("读取期间文件已变化")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil || len(raw) > 64*1024 || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return target, fmt.Errorf("文件不是支持的 UTF-8 文本")
	}
	live, err := parent.Lstat(base)
	if err != nil || !os.SameFile(info, live) || info.ModTime() != live.ModTime() || info.Size() != live.Size() {
		return target, fmt.Errorf("读取期间文件已变化")
	}
	target.fileID, err = directoryIdentity(f)
	if err != nil {
		return target, err
	}
	target.text, target.exists, target.mode = string(raw), true, info.Mode().Perm()
	return target, nil
}

func replaceFile(root *os.Root, name, content string, mode os.FileMode, create bool, recheck func(string) error) error {
	temp := ".noosphere-" + actionID() + ".tmp"
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, err = io.WriteString(f, content)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = f.Sync()
	}
	var replacementID string
	if err == nil {
		replacementID, err = directoryIdentity(f)
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err := recheck(replacementID); err != nil {
		return err
	}
	if create {
		// Link atomically fails if an editor created the destination during
		// the proposal or staging. Rename may overwrite it.
		return root.Link(temp, name)
	}
	return root.Rename(temp, name)
}

package vscode

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Targets maps --target values to the application directory name of each editor.
var Targets = map[string]string{
	"code":     "Code",
	"insiders": "Code - Insiders",
	"vscodium": "VSCodium",
	"cursor":   "Cursor",
}

// KeybindingsPath returns the keybindings.json path of a target editor.
func KeybindingsPath(target string) (string, error) {
	app, ok := Targets[target]
	if !ok {
		return "", fmt.Errorf("unknown target %q (use code, insiders, vscodium or cursor)", target)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, app, "User", "keybindings.json"), nil
}

// BackupSuffix is the timestamp layout used in backup names. It has no colons,
// so it is valid on Windows, and it sorts chronologically.
const BackupSuffix = "2006-01-02T15-04-05"

// ReadCurrent reads keybindings.json. A missing file is returned as nil data and exists=false.
func ReadCurrent(path string) (data []byte, exists bool, err error) {
	data, err = os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Backup copies the current file to <path>.<timestamp>.bak and verifies the copy
// byte for byte against expected (the content the caller based its change on).
// It never overwrites an existing backup.
func Backup(path string, expected []byte, now time.Time) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	base := path + "." + now.Format(BackupSuffix)
	var f *os.File
	var name string
	for i := 1; i < 100; i++ {
		name = base + ".bak"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.bak", base, i)
		}
		f, err = os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("backup failed: %w", err)
		}
	}
	if f == nil {
		return "", errors.New("backup failed: no free backup name")
	}
	if _, err := f.Write(expected); err != nil {
		f.Close()
		return "", fmt.Errorf("backup failed: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", fmt.Errorf("backup failed: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("backup failed: %w", err)
	}
	check, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(check, expected) {
		return "", fmt.Errorf("backup %s could not be verified; nothing was written", name)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, expected) {
		return "", fmt.Errorf("%s changed while the tool was running; nothing was written (backup kept at %s)", path, name)
	}
	return name, nil
}

// WriteResult describes what Write did.
type WriteResult struct {
	Backup  string // "" when there was no file to back up
	Created bool
}

// Write replaces the file at path with data. When the file exists it is first
// backed up and verified (see Backup); if that fails nothing is written.
// expected is the content the new data was derived from; if the file changed
// in the meantime the write is refused. Symlinks are followed, so a
// keybindings.json managed by a dotfiles repository stays a symlink.
func Write(path string, expected, data []byte, existed bool, now time.Time) (WriteResult, error) {
	var res WriteResult
	target := realPath(path)
	dir := filepath.Dir(target)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return res, fmt.Errorf("directory %s does not exist; is the editor installed and started at least once?", dir)
	}
	perm := fs.FileMode(0o644)
	if existed {
		st, err := os.Stat(target)
		if err != nil {
			return res, fmt.Errorf("%s disappeared while the tool was running; nothing was written", path)
		}
		perm = st.Mode().Perm()
		b, err := Backup(target, expected, now)
		if err != nil {
			return res, err
		}
		res.Backup = b
	} else {
		if _, err := os.Lstat(path); err == nil {
			return res, fmt.Errorf("%s appeared while the tool was running; nothing was written", path)
		}
		res.Created = true
	}
	tmp, err := os.CreateTemp(dir, ".keybindings-*.tmp")
	if err != nil {
		return res, err
	}
	tmpName := tmp.Name()
	cleanup := func() { os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return res, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return res, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return res, err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return res, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return res, err
	}
	return res, nil
}

// ListBackups returns backups of path, oldest first. Like Write it follows
// symlinks, because backups are created next to the real file.
func ListBackups(path string) ([]string, error) {
	path = realPath(path)
	matches, err := filepath.Glob(path + ".*.bak")
	if err != nil {
		return nil, err
	}
	prefix := filepath.Base(path) + "."
	var out []string
	for _, m := range matches {
		stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), prefix), ".bak")
		if i := strings.LastIndex(stamp, "-"); i == len(BackupSuffix) {
			stamp = stamp[:i]
		}
		if _, err := time.Parse(BackupSuffix, stamp); err == nil {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ResolveBackup accepts a backup file name or path and checks that it belongs to path.
func ResolveBackup(path, name string) (string, error) {
	list, err := ListBackups(path)
	if err != nil {
		return "", err
	}
	for _, b := range list {
		if b == name || filepath.Base(b) == filepath.Base(name) && filepath.Dir(name) == "." || b == filepath.Clean(name) {
			return b, nil
		}
	}
	return "", fmt.Errorf("%s is not a backup of %s (run restore without arguments to list backups)", name, path)
}

// realPath follows path when the file itself is a symlink (e.g. managed by a
// dotfiles repository). Symlinked parent directories are left as they are.
func realPath(path string) string {
	st, err := os.Lstat(path)
	if err != nil || st.Mode()&fs.ModeSymlink == 0 {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

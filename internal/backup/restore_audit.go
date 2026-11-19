package backup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// auditRecoveredTree is a defense-in-depth check after Kopia has written a
// private restore workspace. It does not replace process/filesystem sandboxing.
func auditRecoveredTree(root string) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return errors.New("invalid restore destination")
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		mode := entry.Type()
		if mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil || filepath.IsAbs(target) {
				return fmt.Errorf("unsafe restored symlink")
			}
			candidate := filepath.Clean(filepath.Join(filepath.Dir(path), target))
			if !pathInside(root, candidate) {
				return fmt.Errorf("restored symlink escapes recovery workspace")
			}
			resolved, err := filepath.EvalSymlinks(path)
			// Reject dangling links and symlink chains that resolve outside root.
			if err != nil || !pathInside(root, resolved) {
				return fmt.Errorf("unsafe symlink target in restored data")
			}
			return nil
		}
		if !entry.IsDir() && !mode.IsRegular() {
			return fmt.Errorf("special file in restore workspace")
		}
		return nil
	})
}

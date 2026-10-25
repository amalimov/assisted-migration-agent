package util

import (
	"errors"
	"fmt"
	"os"
)

// ErrInvalidDirectory indicates that a path does not name an existing directory.
var ErrInvalidDirectory = errors.New("invalid directory")

// IsValidDirectory reports whether path exists and names a directory.
// Invalid paths return an error wrapping ErrInvalidDirectory.
func IsValidDirectory(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("%w: path is empty", ErrInvalidDirectory)
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("%w: path %q does not exist: %w", ErrInvalidDirectory, path, err)
		}
		return false, fmt.Errorf("%w: cannot inspect path %q: %w", ErrInvalidDirectory, path, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%w: path %q is not a directory", ErrInvalidDirectory, path)
	}
	return true, nil
}

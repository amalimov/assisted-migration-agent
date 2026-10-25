package util_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/util"
)

func TestIsValidDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		path   string
		reason string
	}{
		{name: "empty", reason: "path is empty"},
		{name: "missing", path: filepath.Join(dir, "missing"), reason: "does not exist"},
		{name: "file", path: file, reason: "not a directory"},
		{name: "directory", path: dir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := util.IsValidDirectory(tt.path)
			if tt.reason == "" {
				if !valid || err != nil {
					t.Fatalf("valid directory: got valid=%t, err=%v", valid, err)
				}
				return
			}
			if valid || !errors.Is(err, util.ErrInvalidDirectory) || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("invalid directory: got valid=%t, err=%v", valid, err)
			}
		})
	}
}

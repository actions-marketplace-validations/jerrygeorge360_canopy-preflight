package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	target, err := Validate(dir)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if target.Path != dir {
		t.Fatalf("Path = %q, want %q", target.Path, dir)
	}
	if target.Display != filepath.Base(dir) {
		t.Fatalf("Display = %q, want basename %q", target.Display, filepath.Base(dir))
	}
	if strings.Contains(target.Display, filepath.Dir(dir)) {
		t.Fatalf("Display leaks absolute parent path: %q", target.Display)
	}
}

func TestValidateMissingPathDoesNotLeakAbsoluteParent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-project")
	_, err := Validate(missing)
	if err == nil {
		t.Fatal("Validate() succeeded for missing path")
	}
	if !strings.Contains(err.Error(), "target does not exist: missing-project") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), dir) {
		t.Fatalf("error leaks absolute parent path: %v", err)
	}
}

func TestValidateRejectsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	_, err := Validate(path)
	if err == nil || !strings.Contains(err.Error(), "target is not a directory: not-a-directory") {
		t.Fatalf("Validate() error = %v", err)
	}
	if strings.Contains(err.Error(), dir) {
		t.Fatalf("error leaks absolute parent path: %v", err)
	}
}

func TestValidateRelativeDisplay(t *testing.T) {
	target, err := Validate(".")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if target.Display != "." {
		t.Fatalf("Display = %q, want %q", target.Display, ".")
	}
}

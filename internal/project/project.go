// Package project validates inspected project paths without executing or
// modifying their contents.
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Target is a validated project directory. Path is used internally; Display is
// safe to include in reports and never contains an absolute machine path.
type Target struct {
	Path    string
	Display string
}

// Validate confirms that input names an existing directory.
func Validate(input string) (Target, error) {
	if input == "" {
		input = "."
	}
	clean := filepath.Clean(input)
	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return Target{}, fmt.Errorf("target does not exist: %s", displayPath(clean))
		}
		return Target{}, fmt.Errorf("cannot inspect target: %s", displayPath(clean))
	}
	if !info.IsDir() {
		return Target{}, fmt.Errorf("target is not a directory: %s", displayPath(clean))
	}
	return Target{Path: clean, Display: displayPath(clean)}, nil
}

func displayPath(path string) string {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		display := filepath.ToSlash(clean)
		if display != ".." && !strings.HasPrefix(display, "../") && !strings.Contains(display, "\\") {
			return display
		}
	}
	base := filepath.Base(clean)
	if base == string(filepath.Separator) || base == "." {
		return "<root>"
	}
	if strings.Contains(base, "\\") || base == ".." {
		return "<target>"
	}
	return base
}

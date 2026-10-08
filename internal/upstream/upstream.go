// Package upstream collects bounded, immutable Git evidence for fork-drift
// checks. It never fetches, runs hooks, or executes inspected repository code.
package upstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	commandTimeout = 10 * time.Second
	maxCommandOut  = 256 << 10
	maxSourceOut   = 2 << 20
)

// SensitivePaths is the exact evidence-reviewed path set. It is not
// a glob and additions require a new rule-contract review.
var SensitivePaths = []string{
	"fsm/state.go",
	"fsm/automatic.go",
	"fsm/gov.go",
	"fsm/gov_params.go",
	"fsm/transaction.go",
	"fsm/ethereum.go",
	"fsm/key.go",
	"lib/.proto/account.proto",
	"lib/.proto/tx.proto",
	"lib/.proto/plugin.proto",
	"lib/plugin.go",
	"lib/codec.go",
}

// Snapshot is immutable evidence collected from one committed candidate and
// one explicit upstream commit range.
type Snapshot struct {
	BaseSHA                  string
	TargetSHA                string
	CandidateSHA             string
	ChangedSensitivePaths    []string
	CandidateProtocolVersion uint64
	ProtocolVersionLine      int
}

// Inspect validates the candidate repository and gathers the evidence needed
// to compare base through target. base and target must be full SHA-1 commit
// identities; symbolic refs are deliberately unsupported here.
func Inspect(root, base, target string) (Snapshot, error) {
	if !fullSHA(base) || !fullSHA(target) {
		return Snapshot{}, errors.New("upstream base and target must be full 40-character hexadecimal commit SHAs")
	}
	base, target = strings.ToLower(base), strings.ToLower(target)
	if err := exactGitRoot(root); err != nil {
		return Snapshot{}, err
	}
	if shallow, err := gitOutput(root, maxCommandOut, "rev-parse", "--is-shallow-repository"); err != nil {
		return Snapshot{}, errors.New("could not determine whether Git history is complete")
	} else if strings.TrimSpace(shallow) != "false" {
		return Snapshot{}, errors.New("Git history is shallow or incomplete; fetch complete history before comparing immutable commits")
	}
	if dirty, err := gitOutput(root, maxCommandOut, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none"); err != nil {
		return Snapshot{}, errors.New("could not verify that the candidate worktree is clean")
	} else if dirty != "" {
		return Snapshot{}, errors.New("candidate worktree is dirty; commit or remove all changes before comparison")
	}
	candidate, err := gitOutput(root, maxCommandOut, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || !fullSHA(strings.TrimSpace(candidate)) {
		return Snapshot{}, errors.New("candidate HEAD is not a full immutable commit SHA")
	}
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	for _, sha := range []string{base, target, candidate} {
		kind, err := gitOutput(root, maxCommandOut, "cat-file", "-t", sha)
		if err != nil || strings.TrimSpace(kind) != "commit" {
			return Snapshot{}, errors.New("a supplied or candidate commit is missing or is not a commit object")
		}
	}
	if err := gitSuccess(root, "merge-base", "--is-ancestor", base, target); err != nil {
		return Snapshot{}, errors.New("upstream base must be reachable from upstream target")
	}
	if err := gitSuccess(root, "merge-base", "--is-ancestor", base, candidate); err != nil {
		return Snapshot{}, errors.New("upstream base must be reachable from candidate HEAD")
	}
	// Connectivity-only fsck is quiet for complete history, avoiding output
	// proportional to repository size while still detecting missing objects.
	if _, err := gitOutput(root, maxCommandOut, "fsck", "--connectivity-only", "--no-dangling", "--no-reflogs", "--no-progress", base, target, candidate); err != nil {
		return Snapshot{}, errors.New("Git history is incomplete or has missing objects needed for comparison")
	}
	changed, err := changedPaths(root, base, target)
	if err != nil {
		return Snapshot{}, err
	}
	source, err := gitOutput(root, maxSourceOut, "cat-file", "blob", candidate+":fsm/state.go")
	if err != nil {
		return Snapshot{}, errors.New("committed candidate fsm/state.go is unavailable")
	}
	version, line, err := protocolVersion([]byte(source))
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		BaseSHA:                  base,
		TargetSHA:                target,
		CandidateSHA:             candidate,
		ChangedSensitivePaths:    changed,
		CandidateProtocolVersion: version,
		ProtocolVersionLine:      line,
	}, nil
}

func exactGitRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("candidate project directory is unavailable")
	}
	gitRoot, err := gitOutput(root, maxCommandOut, "rev-parse", "--show-toplevel")
	if err != nil {
		return errors.New("candidate project must be a Git worktree root")
	}
	gitInfo, err := os.Stat(strings.TrimSpace(gitRoot))
	if err != nil || !os.SameFile(info, gitInfo) {
		return errors.New("candidate path must be exactly the Git worktree root")
	}
	return nil
}

func changedPaths(root, base, target string) ([]string, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z", base, target, "--"}
	args = append(args, SensitivePaths...)
	output, err := gitOutput(root, maxCommandOut, args...)
	if err != nil {
		return nil, errors.New("could not compare the exact compatibility-sensitive paths")
	}
	if output == "" {
		return []string{}, nil
	}
	allowed := make(map[string]bool, len(SensitivePaths))
	for _, path := range SensitivePaths {
		allowed[path] = true
	}
	parts := strings.Split(output, "\x00")
	if len(parts)%2 != 1 || parts[len(parts)-1] != "" {
		return nil, errors.New("Git returned malformed path metadata for the reviewed comparison")
	}
	paths := make([]string, 0, len(parts)/2)
	seen := make(map[string]bool, len(parts)/2)
	for index := 0; index < len(parts)-1; index += 2 {
		status, path := parts[index], parts[index+1]
		if status != "A" && status != "M" && status != "D" && status != "T" {
			return nil, errors.New("Git returned unsupported path metadata for the reviewed comparison")
		}
		if !allowed[path] || seen[path] {
			return nil, errors.New("Git returned unsupported path metadata for the reviewed comparison")
		}
		seen[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func protocolVersion(source []byte) (uint64, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fsm/state.go", source, 0)
	if err != nil {
		return 0, 0, errors.New("committed candidate fsm/state.go cannot be parsed")
	}
	if file.Name == nil || file.Name.Name != "fsm" {
		return 0, 0, errors.New("committed candidate fsm/state.go must declare package fsm")
	}
	var value uint64
	line := 0
	found := false
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			entry, ok := spec.(*ast.ValueSpec)
			if !ok || len(entry.Names) != 1 || entry.Names[0].Name != "CurrentProtocolVersion" || len(entry.Values) != 1 {
				continue
			}
			literal, literalOK := entry.Values[0].(*ast.BasicLit)
			if entry.Type != nil || !literalOK || literal.Kind != token.INT || found {
				return 0, 0, errors.New("committed candidate fsm/state.go must declare exactly one untyped const CurrentProtocolVersion integer literal")
			}
			parsed, err := strconv.ParseUint(literal.Value, 0, 64)
			if err != nil {
				return 0, 0, errors.New("committed candidate CurrentProtocolVersion is not a valid uint literal")
			}
			value = parsed
			line = fset.Position(entry.Pos()).Line
			found = true
		}
	}
	if !found {
		return 0, 0, errors.New("committed candidate fsm/state.go must declare an untyped const CurrentProtocolVersion integer literal")
	}
	return value, line, nil
}

func fullSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

func gitSuccess(root string, args ...string) error {
	_, err := gitOutput(root, maxCommandOut, args...)
	return err
}

func gitOutput(root string, limit int, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	command := []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "diff.external=",
		"-c", "core.pager=cat",
	}
	command = append(command, args...)
	cmd := exec.CommandContext(ctx, "git", command...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout limitedBuffer
	stdout.limit = limit
	var stderr limitedBuffer
	stderr.limit = 4096
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", errors.New("Git command exceeded the inspection timeout")
	}
	if errors.Is(stdout.err, errOutputLimit) || errors.Is(stderr.err, errOutputLimit) {
		return "", errors.New("Git command output exceeded the inspection limit")
	}
	if err != nil {
		return "", fmt.Errorf("Git command failed: %w", err)
	}
	return stdout.String(), nil
}

var errOutputLimit = errors.New("command output limit exceeded")

type limitedBuffer struct {
	bytes.Buffer
	limit int
	err   error
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	if buffer.Len()+len(data) > buffer.limit {
		remaining := buffer.limit - buffer.Len()
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(data[:remaining])
		}
		buffer.err = errOutputLimit
		return 0, errOutputLimit
	}
	return buffer.Buffer.Write(data)
}

var _ io.Writer = (*limitedBuffer)(nil)

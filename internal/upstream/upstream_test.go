package upstream

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInspectIgnoresDocumentationOnlyDriftAndNormalizesSHAs(t *testing.T) {
	repo := newRepository(t, validState(2))
	base := repo.head()
	repo.write("README.md", "documentation only\n")
	repo.write("fsm/automatic.md", "nearby documentation\n")
	repo.write("fsm/state.go.bak", "near match\n")
	repo.write("nested/fsm/state.go", "nested mirror\n")
	target := repo.commit("docs")

	got, err := Inspect(repo.dir, strings.ToUpper(base), strings.ToUpper(target))
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if got.BaseSHA != base || got.TargetSHA != target || got.CandidateSHA != target {
		t.Fatalf("immutable identities = %#v", got)
	}
	if got.CandidateProtocolVersion != 2 || got.ProtocolVersionLine != 3 {
		t.Fatalf("protocol evidence = %#v", got)
	}
	if got.ChangedSensitivePaths == nil || len(got.ChangedSensitivePaths) != 0 {
		t.Fatalf("changed paths = %#v, want non-nil empty", got.ChangedSensitivePaths)
	}

	again, err := Inspect(repo.dir, base, target)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("repeated Inspect() = %#v, %v; want %#v", again, err, got)
	}
}

func TestInspectReportsEveryExactSensitivePathInSortedOrder(t *testing.T) {
	repo := newRepository(t, validState(2))
	for _, path := range SensitivePaths {
		if path != "fsm/state.go" {
			repo.write(path, "baseline\n")
		}
	}
	repo.commit("sensitive baseline")
	base := repo.head()
	for _, path := range SensitivePaths {
		if path == "fsm/state.go" {
			repo.write(path, validState(2)+"// changed\n")
		} else {
			repo.write(path, "changed\n")
		}
	}
	target := repo.commit("all sensitive paths")

	got, err := Inspect(repo.dir, base, target)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	want := append([]string(nil), SensitivePaths...)
	sortStrings(want)
	if !reflect.DeepEqual(got.ChangedSensitivePaths, want) {
		t.Fatalf("changed paths = %#v, want %#v", got.ChangedSensitivePaths, want)
	}
}

func TestInspectRejectsNonImmutableReferences(t *testing.T) {
	repo := newRepository(t, validState(2))
	sha := repo.head()
	for _, test := range []struct {
		name, base, target string
	}{
		{name: "symbolic", base: "HEAD", target: sha},
		{name: "short", base: sha[:12], target: sha},
		{name: "malformed", base: strings.Repeat("z", 40), target: sha},
		{name: "unresolved", base: strings.Repeat("0", 40), target: sha},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Inspect(repo.dir, test.base, test.target); err == nil {
				t.Fatal("Inspect() error = nil, want immutable-input error")
			}
		})
	}
}

func TestInspectRejectsAnnotatedTagObjectIdentity(t *testing.T) {
	repo := newRepository(t, validState(2))
	commit := repo.head()
	repo.git("tag", "-a", "candidate-tag", "-m", "annotated")
	tagObject := strings.TrimSpace(repo.git("rev-parse", "candidate-tag^{tag}"))
	if _, err := Inspect(repo.dir, tagObject, commit); err == nil || !strings.Contains(err.Error(), "commit object") {
		t.Fatalf("Inspect() error = %v, want direct commit-object rejection", err)
	}
}

func TestInspectIgnoresGitReplacementObjects(t *testing.T) {
	repo := newRepository(t, validState(2))
	base := repo.head()
	repo.write("fsm/state.go", validState(1))
	candidate := repo.commit("alternate version")
	repo.git("replace", candidate, base)
	got, err := Inspect(repo.dir, base, base)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if got.CandidateSHA != candidate || got.CandidateProtocolVersion != 1 {
		t.Fatalf("replacement object changed immutable evidence: %#v", got)
	}
}

func TestInspectTreatsSensitivePathTypeChangeAsDrift(t *testing.T) {
	repo := newRepository(t, validState(2))
	repo.write("lib/plugin.go", "package lib\n")
	base := repo.commit("regular sensitive file")
	if err := os.Remove(filepath.Join(repo.dir, "lib", "plugin.go")); err != nil {
		t.Fatalf("remove regular file: %v", err)
	}
	if err := os.Symlink("../README.md", filepath.Join(repo.dir, "lib", "plugin.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := repo.commit("change sensitive file type")
	got, err := Inspect(repo.dir, base, target)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !reflect.DeepEqual(got.ChangedSensitivePaths, []string{"lib/plugin.go"}) {
		t.Fatalf("changed paths = %#v", got.ChangedSensitivePaths)
	}
}

func TestInspectRejectsDirtyCandidate(t *testing.T) {
	for _, test := range []struct {
		name  string
		dirty func(*testRepository)
	}{
		{name: "unstaged", dirty: func(repo *testRepository) { repo.write("README.md", "edited\n") }},
		{name: "staged", dirty: func(repo *testRepository) { repo.write("staged.txt", "staged\n"); repo.git("add", "staged.txt") }},
		{name: "untracked", dirty: func(repo *testRepository) { repo.write("untracked.txt", "untracked\n") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newRepository(t, validState(2))
			sha := repo.head()
			test.dirty(repo)
			_, err := Inspect(repo.dir, sha, sha)
			if err == nil || !strings.Contains(err.Error(), "dirty") {
				t.Fatalf("Inspect() error = %v, want dirty candidate", err)
			}
		})
	}
}

func TestInspectRejectsIncompleteAndInvalidAncestry(t *testing.T) {
	t.Run("base not ancestor of target", func(t *testing.T) {
		repo := newRepository(t, validState(2))
		root := repo.head()
		repo.write("base.txt", "base\n")
		base := repo.commit("base branch")
		repo.git("checkout", "--detach", root)
		repo.write("target.txt", "target\n")
		target := repo.commit("target branch")
		if _, err := Inspect(repo.dir, base, target); err == nil || !strings.Contains(err.Error(), "reachable") {
			t.Fatalf("Inspect() error = %v, want ancestry error", err)
		}
	})

	t.Run("base not ancestor of candidate", func(t *testing.T) {
		repo := newRepository(t, validState(2))
		root := repo.head()
		repo.write("base.txt", "base\n")
		base := repo.commit("base")
		repo.write("target.txt", "target\n")
		target := repo.commit("target")
		repo.git("checkout", "--detach", root)
		repo.write("candidate.txt", "candidate\n")
		repo.commit("candidate sibling")
		if _, err := Inspect(repo.dir, base, target); err == nil {
			t.Fatal("Inspect() error = nil, want base-to-candidate ancestry error")
		}
	})

	t.Run("shallow", func(t *testing.T) {
		source := newRepository(t, validState(2))
		base := source.head()
		source.write("README.md", "next\n")
		target := source.commit("next")
		clone := filepath.Join(t.TempDir(), "clone")
		runGit(t, "", "clone", "--depth=1", "file://"+source.dir, clone)
		if _, err := Inspect(clone, base, target); err == nil || !strings.Contains(err.Error(), "shallow") {
			t.Fatalf("Inspect() error = %v, want shallow-history error", err)
		}
	})
}

func TestInspectRequiresExactSupportedProtocolDeclaration(t *testing.T) {
	tests := []struct{ name, source string }{
		{name: "missing", source: "package fsm\nconst Other uint = 2\n"},
		{name: "explicit type", source: "package fsm\nconst CurrentProtocolVersion uint = 2\n"},
		{name: "expression", source: "package fsm\nconst CurrentProtocolVersion uint = 1 + 1\n"},
		{name: "iota", source: "package fsm\nconst CurrentProtocolVersion uint = iota\n"},
		{name: "wrong type", source: "package fsm\nconst CurrentProtocolVersion uint64 = 2\n"},
		{name: "non integer", source: "package fsm\nconst CurrentProtocolVersion uint = 2.0\n"},
		{name: "wrong package", source: "package impostor\nconst CurrentProtocolVersion uint = 2\n"},
		{name: "duplicate", source: "package fsm\nconst CurrentProtocolVersion uint = 2\nconst CurrentProtocolVersion uint = 3\n"},
		{name: "malformed", source: "package fsm\nconst (\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newRepository(t, validState(2))
			base := repo.head()
			repo.write("fsm/state.go", test.source)
			target := repo.commit("unsupported protocol declaration")
			if _, err := Inspect(repo.dir, base, target); err == nil {
				t.Fatal("Inspect() error = nil, want unsupported declaration error")
			}
		})
	}
}

func TestInspectRequiresExactRepositoryRoot(t *testing.T) {
	repo := newRepository(t, validState(2))
	sha := repo.head()
	child := filepath.Join(repo.dir, "fsm")
	if _, err := Inspect(child, sha, sha); err == nil || !strings.Contains(err.Error(), "exactly") {
		t.Fatalf("Inspect() error = %v, want exact-root error", err)
	}
}

func validState(version uint64) string {
	return "package fsm\n\nconst CurrentProtocolVersion = " + uintString(version) + "\n"
}

func uintString(value uint64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var result [20]byte
	i := len(result)
	for value > 0 {
		i--
		result[i] = digits[value%10]
		value /= 10
	}
	return string(result[i:])
}

type testRepository struct {
	t   *testing.T
	dir string
}

func newRepository(t *testing.T, state string) *testRepository {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	repo := &testRepository{t: t, dir: t.TempDir()}
	repo.git("init", "--quiet")
	repo.git("config", "user.name", "Canopy Doctor Tests")
	repo.git("config", "user.email", "doctor@example.invalid")
	repo.write("fsm/state.go", state)
	repo.commit("initial")
	return repo
}

func (repo *testRepository) write(path, content string) {
	repo.t.Helper()
	full := filepath.Join(repo.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		repo.t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		repo.t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func (repo *testRepository) commit(message string) string {
	repo.t.Helper()
	repo.git("add", "-A")
	repo.git("commit", "--quiet", "-m", message)
	return repo.head()
}

func (repo *testRepository) head() string {
	repo.t.Helper()
	return strings.TrimSpace(repo.git("rev-parse", "HEAD"))
}

func (repo *testRepository) git(args ...string) string {
	repo.t.Helper()
	return runGit(repo.t, repo.dir, args...)
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

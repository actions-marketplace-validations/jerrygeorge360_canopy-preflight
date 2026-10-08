package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
	"github.com/jerrygeorge360/canopy-preflight/internal/report"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantFormat string
		wantPath   string
		wantError  string
	}{
		{name: "defaults", args: []string{"check"}, wantFormat: "human", wantPath: "."},
		{name: "separate format", args: []string{"check", "--format", "json", "project"}, wantFormat: "json", wantPath: "project"},
		{name: "equals format", args: []string{"check", "--format=json", "project"}, wantFormat: "json", wantPath: "project"},
		{name: "missing command", wantError: "expected the check command"},
		{name: "bad command", args: []string{"scan"}, wantError: "expected the check command"},
		{name: "bad format", args: []string{"check", "--format", "xml"}, wantError: `unsupported format "xml"`},
		{name: "missing format", args: []string{"check", "--format"}, wantError: "--format requires a value"},
		{name: "duplicate format", args: []string{"check", "--format=json", "--format", "human"}, wantError: "--format may be provided only once"},
		{name: "unknown option", args: []string{"check", "--wat"}, wantError: `unknown option "--wat"`},
		{name: "two paths", args: []string{"check", "one", "two"}, wantError: "only one project path may be provided"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parse(test.args)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("parse() error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse() error = %v", err)
			}
			if got.format != test.wantFormat || got.path != test.wantPath {
				t.Fatalf("parse() = %#v, want format=%q path=%q", got, test.wantFormat, test.wantPath)
			}
		})
	}
}

func TestRunHumanReviewsEmptyTarget(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"check", dir}, &stdout, &stderr, "test")
	if code != 2 {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Target: "+filepath.Base(dir)+"\nDecision: REVIEW REQUIRED\n") || !strings.Contains(stdout.String(), "Plugin configuration was not found.") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunPrintsVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, "v0.2.0"); code != 0 {
			t.Fatalf("Run(%v) = %d, stderr = %q", args, code, stderr.String())
		}
		if stdout.String() != "canopy-doctor v0.2.0\n" || stderr.Len() != 0 {
			t.Fatalf("Run(%v): stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

func TestRunPrintsHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr, "test"); code != 0 {
			t.Fatalf("Run(%v) = %d, stderr = %q", args, code, stderr.String())
		}
		if stdout.String() != usage+"\n" || stderr.Len() != 0 {
			t.Fatalf("Run(%v): stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

func TestRunJSONReviewsEmptyTarget(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"check", "--format=json", dir}, &stdout, &stderr, "test")
	if code != 2 {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
	var got report.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if got.SchemaVersion != report.SchemaVersion || got.Tool.Version != "test" || got.Target != filepath.Base(dir) || got.Decision != diagnostic.DecisionReview || len(got.Findings) != 1 {
		t.Fatalf("unexpected report: %#v", got)
	}
}

func TestRunReviewsNonGoTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not Go"), 0o600); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"check", dir}, &stdout, &stderr, "test"); code != 2 {
		t.Fatalf("Run() = %d, want 2; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Decision: REVIEW REQUIRED\n") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunReportsUsageErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"bad-command"}, &stdout, &stderr, "test")
	if code != 1 {
		t.Fatalf("Run() = %d, want 1", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "expected the check command") || !strings.Contains(stderr.String(), usage) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestGuardedRunConvertsUnexpectedPanicToOperationalError(t *testing.T) {
	var stderr bytes.Buffer
	code := guardedRun(&stderr, func() int {
		panic("sensitive internal detail")
	})
	if code != 1 {
		t.Fatalf("guardedRun() = %d, want 1", code)
	}
	if stderr.String() != "error: internal inspection failure\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunRejectsMissingAndFileTargets(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "missing", path: filepath.Join(dir, "missing"), want: "target does not exist: missing"},
		{name: "file", path: filepath.Join(dir, "file"), want: "target is not a directory: file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "file" {
				if err := os.WriteFile(test.path, []byte("fixture"), 0o600); err != nil {
					t.Fatalf("create fixture: %v", err)
				}
			}
			var stdout, stderr bytes.Buffer
			if code := Run([]string{"check", test.path}, &stdout, &stderr, "test"); code != 1 {
				t.Fatalf("Run() = %d, want 1", code)
			}
			if !strings.Contains(stderr.String(), test.want) || strings.Contains(stderr.String(), dir) {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunReturnsOperationalErrorWhenOutputFails(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	code := Run([]string{"check", dir}, errorWriter{}, &stderr, "test")
	if code != 1 {
		t.Fatalf("Run() = %d, want 1", code)
	}
	if stderr.String() != "error: could not write report\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPhase2FixturesAgreeAcrossFormats(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		wantCode int
		want     diagnostic.Decision
	}{
		{name: "valid", fixture: "pass", wantCode: 0, want: diagnostic.DecisionPass},
		{name: "review", fixture: "review", wantCode: 2, want: diagnostic.DecisionReview},
		{name: "one-hop mutable prefix", fixture: "one-hop", wantCode: 2, want: diagnostic.DecisionReview},
		{name: "blocking", fixture: "block", wantCode: 3, want: diagnostic.DecisionBlock},
		{name: "malformed descriptor", fixture: "malformed", wantCode: 2, want: diagnostic.DecisionReview},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "phase2", test.fixture)
			var human, humanErr bytes.Buffer
			if code := Run([]string{"check", path}, &human, &humanErr, "test"); code != test.wantCode {
				t.Fatalf("human exit = %d, want %d; stderr=%q\n%s", code, test.wantCode, humanErr.String(), human.String())
			}
			var machine, machineErr bytes.Buffer
			if code := Run([]string{"check", "--format=json", path}, &machine, &machineErr, "test"); code != test.wantCode {
				t.Fatalf("JSON exit = %d, want %d; stderr=%q\n%s", code, test.wantCode, machineErr.String(), machine.String())
			}
			var decoded report.Report
			if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
				t.Fatalf("decode JSON: %v\n%s", err, machine.String())
			}
			if decoded.Decision != test.want {
				t.Fatalf("JSON decision = %q, want %q", decoded.Decision, test.want)
			}
			if !strings.Contains(human.String(), "Decision: "+string(decoded.Decision)+"\n") {
				t.Fatalf("human/JSON decisions differ:\n%s\n%s", human.String(), machine.String())
			}
		})
	}
}

func TestRunReturnsOperationalErrorForOversizedSource(t *testing.T) {
	dir := t.TempDir()
	data := append([]byte("package fixture\n"), bytes.Repeat([]byte{' '}, (4<<20)+1)...)
	if err := os.WriteFile(filepath.Join(dir, "large.go"), data, 0o600); err != nil {
		t.Fatalf("create oversized source: %v", err)
	}
	for _, format := range []string{"human", "json"} {
		t.Run(format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := []string{"check", fmt.Sprintf("--format=%s", format), dir}
			if code := Run(args, &stdout, &stderr, "test"); code != 1 {
				t.Fatalf("Run() = %d, want 1", code)
			}
			if stdout.Len() != 0 || stderr.String() != "error: could not inspect target\n" {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunCNPY003FixturesAgreeAcrossFormats(t *testing.T) {
	tests := []struct {
		fixture  string
		wantCode int
		want     diagnostic.Decision
		finding  string
	}{
		{fixture: "valid", wantCode: 0, want: diagnostic.DecisionPass},
		{fixture: "wrong-type", wantCode: 3, want: diagnostic.DecisionBlock, finding: "CNPY003-NONCE-CONFLICT"},
		{fixture: "repeated", wantCode: 3, want: diagnostic.DecisionBlock, finding: "CNPY003-NONCE-CONFLICT"},
		{fixture: "omitted", wantCode: 2, want: diagnostic.DecisionReview, finding: "CNPY003-NONCE-REVIEW"},
		{fixture: "unrelated", wantCode: 2, want: diagnostic.DecisionReview, finding: "CNPY003-ACCOUNT-UNKNOWN"},
		{fixture: "field-10", wantCode: 2, want: diagnostic.DecisionReview, finding: "CNPY003-NONCE-REVIEW"},
		{fixture: "malformed", wantCode: 3, want: diagnostic.DecisionBlock, finding: "CNPY003-INVALID-DESCRIPTOR"},
		{fixture: "unresolvable", wantCode: 2, want: diagnostic.DecisionReview, finding: "CNPY001-CONFIG-REVIEW"},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "phase3", test.fixture)
			var human, humanErr bytes.Buffer
			if code := Run([]string{"check", path}, &human, &humanErr, "test"); code != test.wantCode {
				t.Fatalf("human exit = %d, want %d; stderr=%q\n%s", code, test.wantCode, humanErr.String(), human.String())
			}
			var machine, machineErr bytes.Buffer
			if code := Run([]string{"check", "--format=json", path}, &machine, &machineErr, "test"); code != test.wantCode {
				t.Fatalf("JSON exit = %d, want %d; stderr=%q\n%s", code, test.wantCode, machineErr.String(), machine.String())
			}
			var decoded report.Report
			if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
				t.Fatalf("decode JSON: %v\n%s", err, machine.String())
			}
			if decoded.Decision != test.want || !strings.Contains(human.String(), "Decision: "+string(decoded.Decision)+"\n") {
				t.Fatalf("human/JSON decisions differ:\n%s\n%s", human.String(), machine.String())
			}
			if test.finding != "" {
				if !strings.Contains(human.String(), test.finding) || !reportHasCode(decoded, test.finding) {
					t.Fatalf("finding %q missing from reports:\n%s\n%s", test.finding, human.String(), machine.String())
				}
			}
			var repeated bytes.Buffer
			if code := Run([]string{"check", "--format=json", path}, &repeated, &machineErr, "test"); code != test.wantCode || repeated.String() != machine.String() {
				t.Fatal("repeated JSON output is not deterministic")
			}
		})
	}
}

func TestParseCNPY004Options(t *testing.T) {
	upperBase := strings.Repeat("A", 40)
	upperTarget := strings.Repeat("B", 40)
	got, err := parse([]string{
		"check", "--upstream-base=" + upperBase, "--upstream-target", upperTarget,
		"--deployment-height", "100", "--activation-height=99", "--required-protocol-version", "2", "project",
	})
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if got.upstreamBase != strings.ToLower(upperBase) || got.upstreamTarget != strings.ToLower(upperTarget) ||
		!got.hasReleaseContext || got.deploymentHeight != 100 || got.activationHeight != 99 || got.requiredProtocolVersion != 2 || got.path != "project" {
		t.Fatalf("parse() = %#v", got)
	}
}

func TestParseRejectsIncompleteOrMutableCNPY004Inputs(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "base only", args: []string{"check", "--upstream-base", sha}, want: "must be supplied together"},
		{name: "target only", args: []string{"check", "--upstream-target", sha}, want: "must be supplied together"},
		{name: "symbolic base", args: []string{"check", "--upstream-base", "HEAD", "--upstream-target", sha}, want: "full 40-character"},
		{name: "short target", args: []string{"check", "--upstream-base", sha, "--upstream-target", sha[:12]}, want: "full 40-character"},
		{name: "partial context", args: []string{"check", "--deployment-height", "1"}, want: "must be supplied together"},
		{name: "negative height", args: []string{"check", "--deployment-height=-1", "--activation-height=1", "--required-protocol-version=2"}, want: "unsigned decimal"},
		{name: "hex version", args: []string{"check", "--deployment-height=1", "--activation-height=1", "--required-protocol-version=0x2"}, want: "unsigned decimal"},
		{name: "overflow", args: []string{"check", "--deployment-height=18446744073709551616", "--activation-height=1", "--required-protocol-version=2"}, want: "unsigned decimal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parse(test.args); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parse() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRunCNPY004HumanAndJSONAgreement(t *testing.T) {
	tests := []struct {
		name       string
		version    uint64
		path       string
		context    bool
		deployment uint64
		activation uint64
		required   uint64
		wantExit   int
		want       diagnostic.Decision
		wantCode   string
	}{
		{name: "docs only pass", version: 2, path: "README.md", context: true, deployment: 100, activation: 100, required: 2, wantExit: 0, want: diagnostic.DecisionPass, wantCode: "CNPY004-CLEAN"},
		{name: "sensitive review", version: 2, path: "lib/plugin.go", context: true, deployment: 99, activation: 100, required: 3, wantExit: 2, want: diagnostic.DecisionReview, wantCode: "CNPY004-SENSITIVE-DRIFT"},
		{name: "context review", version: 2, path: "README.md", context: false, wantExit: 2, want: diagnostic.DecisionReview, wantCode: "CNPY004-CONTEXT-REVIEW"},
		{name: "active block", version: 1, path: "README.md", context: true, deployment: 100, activation: 100, required: 2, wantExit: 3, want: diagnostic.DecisionBlock, wantCode: "CNPY004-ACTIVE-VERSION"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newPhase4CLIRepository(t, test.version)
			base := repo.head()
			repo.write(test.path, "changed\n")
			target := repo.commit("comparison target")
			args := []string{"check", "--upstream-base", base, "--upstream-target", target}
			if test.context {
				args = append(args,
					"--deployment-height", fmt.Sprint(test.deployment),
					"--activation-height", fmt.Sprint(test.activation),
					"--required-protocol-version", fmt.Sprint(test.required),
				)
			}
			args = append(args, repo.dir)

			var human, humanErr bytes.Buffer
			if code := Run(args, &human, &humanErr, "test"); code != test.wantExit {
				t.Fatalf("human exit = %d, want %d; stderr=%q\n%s", code, test.wantExit, humanErr.String(), human.String())
			}
			jsonArgs := append([]string{"check", "--format=json"}, args[1:]...)
			var machine, machineErr bytes.Buffer
			if code := Run(jsonArgs, &machine, &machineErr, "test"); code != test.wantExit {
				t.Fatalf("JSON exit = %d, want %d; stderr=%q\n%s", code, test.wantExit, machineErr.String(), machine.String())
			}
			var decoded report.Report
			if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
				t.Fatalf("decode JSON: %v\n%s", err, machine.String())
			}
			if decoded.Decision != test.want || !reportHasCode(decoded, test.wantCode) || !strings.Contains(human.String(), test.wantCode) ||
				!strings.Contains(human.String(), "Decision: "+string(decoded.Decision)+"\n") {
				t.Fatalf("human/JSON disagree:\n%s\n%s", human.String(), machine.String())
			}
			for _, sha := range []string{base, target, repo.head()} {
				if !strings.Contains(human.String(), sha) || !strings.Contains(machine.String(), sha) {
					t.Fatalf("immutable SHA %s omitted:\n%s\n%s", sha, human.String(), machine.String())
				}
			}
			if strings.Contains(human.String(), repo.dir) || strings.Contains(machine.String(), repo.dir) {
				t.Fatalf("machine-specific path leaked:\n%s\n%s", human.String(), machine.String())
			}
			var repeated bytes.Buffer
			if code := Run(jsonArgs, &repeated, &machineErr, "test"); code != test.wantExit || repeated.String() != machine.String() {
				t.Fatal("repeated JSON output is not deterministic")
			}
		})
	}
}

func TestRunCNPY004DirtyCandidateIsOperationalError(t *testing.T) {
	repo := newPhase4CLIRepository(t, 2)
	sha := repo.head()
	repo.write("untracked.txt", "dirty\n")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"check", "--upstream-base", sha, "--upstream-target", sha, repo.dir}, &stdout, &stderr, "test")
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "candidate worktree is dirty") || strings.Contains(stderr.String(), repo.dir) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

type phase4CLIRepository struct {
	t   *testing.T
	dir string
}

func newPhase4CLIRepository(t *testing.T, version uint64) *phase4CLIRepository {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	repo := &phase4CLIRepository{t: t, dir: t.TempDir()}
	repo.git("init", "--quiet")
	repo.git("config", "user.name", "Canopy Doctor Tests")
	repo.git("config", "user.email", "doctor@example.invalid")
	for _, name := range []string{"config.go", "demo.pb.go"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "phase2", "pass", name))
		if err != nil {
			t.Fatalf("read passing fixture: %v", err)
		}
		repo.write(name, string(data))
	}
	repo.write("fsm/state.go", fmt.Sprintf("package fsm\n\nconst CurrentProtocolVersion = %d\n", version))
	repo.commit("initial")
	return repo
}

func (repo *phase4CLIRepository) write(path, content string) {
	repo.t.Helper()
	full := filepath.Join(repo.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		repo.t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		repo.t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func (repo *phase4CLIRepository) commit(message string) string {
	repo.t.Helper()
	repo.git("add", "-A")
	repo.git("commit", "--quiet", "-m", message)
	return repo.head()
}

func (repo *phase4CLIRepository) head() string {
	repo.t.Helper()
	return strings.TrimSpace(repo.git("rev-parse", "HEAD"))
}

func (repo *phase4CLIRepository) git(args ...string) string {
	repo.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
	output, err := cmd.CombinedOutput()
	if err != nil {
		repo.t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func reportHasCode(value report.Report, code string) bool {
	for _, finding := range value.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("fixture write failure") }

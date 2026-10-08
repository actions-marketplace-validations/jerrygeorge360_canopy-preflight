package upstreamwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCheckFindsReleaseAndSensitiveChanges(t *testing.T) {
	const baselineSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const mainSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/repos/canopy-network/canopy/releases/latest":
			_, _ = writer.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://example.test/releases/v0.2.0"}`))
		case "/repos/canopy-network/canopy/branches/main":
			_, _ = writer.Write([]byte(`{"commit":{"sha":"` + mainSHA + `"}}`))
		case "/repos/canopy-network/canopy/branches/development":
			_, _ = writer.Write([]byte(`{"commit":{"sha":"` + baselineSHA + `"}}`))
		case "/repos/canopy-network/canopy/compare/" + baselineSHA + "..." + mainSHA:
			_, _ = writer.Write([]byte(`{"status":"ahead","total_commits":3,"files":[{"filename":"README.md","status":"modified"},{"filename":"lib/codec.go","status":"modified"}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	report, err := (Client{HTTP: server.Client(), BaseURL: server.URL}).Check(context.Background(), Baseline{
		Repository:      "canopy-network/canopy",
		ReviewedCommit:  baselineSHA,
		ReviewedRelease: "v0.1.0",
		Branches:        []string{"main", "development"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.ActionRequired {
		t.Fatal("expected upstream review to be required")
	}
	if got := report.Branches[0].Changes; len(got) != 1 || got[0].Path != "lib/codec.go" {
		t.Fatalf("sensitive changes = %#v", got)
	}
	wantRules := []string{"CNPY001", "CNPY003", "CNPY004"}
	if !reflect.DeepEqual(report.Branches[0].Changes[0].Rules, wantRules) {
		t.Fatalf("rules = %#v, want %#v", report.Branches[0].Changes[0].Rules, wantRules)
	}
	if !strings.Contains(report.Markdown, IssueMarker) || !strings.Contains(report.Markdown, "lib/codec.go") {
		t.Fatalf("unexpected markdown:\n%s", report.Markdown)
	}
}

func TestClassifyLanguageContractAndOrdinaryFile(t *testing.T) {
	if got := classify("plugin/python/TUTORIAL.md"); !reflect.DeepEqual(got, []string{"CNPY001", "CNPY002", "CNPY003"}) {
		t.Fatalf("language tutorial rules = %#v", got)
	}
	if got := classify("cmd/rpc/server.go"); len(got) != 0 {
		t.Fatalf("ordinary file rules = %#v", got)
	}
}

func TestLoadBaselineRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	contents := `{"repository":"canopy-network/canopy","reviewed_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","branches":["main"],"unknown":true}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBaseline(path); err == nil {
		t.Fatal("expected unknown baseline field to fail")
	}
}

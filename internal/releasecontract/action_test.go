// Package releasecontract verifies the repository-owned GitHub Action surface.
package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActionContractBuildsFromActionPathAndUsesArgumentArray(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatalf("read action.yml: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		"using: composite",
		"default: human",
		"actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff",
		"go-version-file: ${{ github.action_path }}/go.mod",
		"cd \"$ACTION_PATH\"",
		"go build -buildvcs=false",
		"args=(check \"--format=${CANOPY_DOCTOR_FORMAT}\")",
		"\"$CANOPY_DOCTOR_BINARY\" \"${args[@]}\"",
		"upstream-base:",
		"upstream-target:",
		"deployment-height:",
		"activation-height:",
		"required-protocol-version:",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("action.yml omits required contract %q", required)
		}
	}
}

func TestReleaseWorkflowContractPublishesChecksummedPlatformArtifacts(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		"- \"v*\"",
		"goos: linux",
		"goos: darwin",
		"goos: windows",
		"goarch: amd64",
		"goarch: arm64",
		"make dist VERSION=\"${GITHUB_REF_NAME}\"",
		"sha256sum canopy-doctor-* > checksums.txt",
		"cp ../LICENSE ../NOTICE .",
		"contents: write",
		"gh release create",
		"dist/LICENSE dist/NOTICE",
		"release tag must use vMAJOR.MINOR.PATCH",
		"git merge-base --is-ancestor",
		"actions/checkout@11d5960a326750d5838078e36cf38b85af677262",
		"actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff",
		"actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02",
		"actions/download-artifact@d3f86a106a0bac45b974a628896c90dbdf5c8093",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("release.yml omits required contract %q", required)
		}
	}
}

func TestCIExecutesCompositeActionAgainstPassingFixture(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		"action-integration:",
		"uses: ./",
		"path: testdata/phase2/pass",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("ci.yml omits Action integration contract %q", required)
		}
	}
}

func TestDistributionIncludesApacheLicenseAndCopyrightNotice(t *testing.T) {
	license, err := os.ReadFile(filepath.Join("..", "..", "LICENSE"))
	if err != nil {
		t.Fatalf("read LICENSE: %v", err)
	}
	if !strings.Contains(string(license), "Apache License") ||
		!strings.Contains(string(license), "Version 2.0, January 2004") {
		t.Fatal("LICENSE is not the Apache License 2.0 text")
	}

	notice, err := os.ReadFile(filepath.Join("..", "..", "NOTICE"))
	if err != nil {
		t.Fatalf("read NOTICE: %v", err)
	}
	if !strings.Contains(string(notice), "Copyright 2026 jerrygeorge360") {
		t.Fatal("NOTICE omits the selected copyright holder")
	}
}

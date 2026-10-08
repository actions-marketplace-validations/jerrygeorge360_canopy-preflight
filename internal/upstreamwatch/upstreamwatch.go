// Package upstreamwatch detects changes to Canopy compatibility evidence.
package upstreamwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
)

const IssueMarker = "<!-- canopy-upstream-watch -->"

var (
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	commitPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Baseline records the newest Canopy source state that has been reviewed.
type Baseline struct {
	Repository      string   `json:"repository"`
	ReviewedCommit  string   `json:"reviewed_commit"`
	ReviewedRelease string   `json:"reviewed_release"`
	Branches        []string `json:"branches"`
}

// Change is one compatibility-sensitive upstream file change.
type Change struct {
	Path         string   `json:"path"`
	Status       string   `json:"status"`
	PreviousPath string   `json:"previous_path,omitempty"`
	Rules        []string `json:"rules"`
}

// BranchReport describes one upstream branch comparison.
type BranchReport struct {
	Name         string   `json:"name"`
	HeadCommit   string   `json:"head_commit"`
	CompareURL   string   `json:"compare_url"`
	Status       string   `json:"status"`
	TotalCommits int      `json:"total_commits"`
	Truncated    bool     `json:"truncated"`
	Changes      []Change `json:"sensitive_changes"`
}

// Release records the latest upstream release.
type Release struct {
	Tag string `json:"tag"`
	URL string `json:"url"`
}

// Report is written for the workflow and its review issue.
type Report struct {
	Repository      string         `json:"repository"`
	ReviewedCommit  string         `json:"reviewed_commit"`
	ReviewedRelease string         `json:"reviewed_release"`
	LatestRelease   Release        `json:"latest_release"`
	Branches        []BranchReport `json:"branches"`
	Reasons         []string       `json:"reasons"`
	ActionRequired  bool           `json:"action_required"`
	Markdown        string         `json:"markdown"`
}

// Client queries the public GitHub API. BaseURL can be replaced in tests.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

type branchResponse struct {
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type compareResponse struct {
	Status       string `json:"status"`
	TotalCommits int    `json:"total_commits"`
	Files        []struct {
		Filename         string `json:"filename"`
		Status           string `json:"status"`
		PreviousFilename string `json:"previous_filename"`
	} `json:"files"`
}

// LoadBaseline reads and strictly validates a baseline file.
func LoadBaseline(path string) (Baseline, error) {
	file, err := os.Open(path)
	if err != nil {
		return Baseline{}, fmt.Errorf("open upstream baseline: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	var baseline Baseline
	if err := decoder.Decode(&baseline); err != nil {
		return Baseline{}, fmt.Errorf("decode upstream baseline: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Baseline{}, err
	}
	if err := baseline.Validate(); err != nil {
		return Baseline{}, err
	}
	return baseline, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("upstream baseline contains more than one JSON value")
		}
		return fmt.Errorf("decode upstream baseline: %w", err)
	}
	return nil
}

// Validate rejects ambiguous repositories, commits, and branch lists.
func (baseline Baseline) Validate() error {
	if !repositoryPattern.MatchString(baseline.Repository) {
		return errors.New("upstream repository must use owner/name format")
	}
	if !commitPattern.MatchString(baseline.ReviewedCommit) {
		return errors.New("reviewed_commit must be a lowercase 40-character SHA")
	}
	if len(baseline.Branches) == 0 {
		return errors.New("at least one upstream branch is required")
	}
	seen := make(map[string]bool, len(baseline.Branches))
	for _, branch := range baseline.Branches {
		if branch == "" || strings.ContainsAny(branch, " \t\r\n") {
			return errors.New("upstream branch names must be non-empty and contain no whitespace")
		}
		if seen[branch] {
			return fmt.Errorf("duplicate upstream branch %q", branch)
		}
		seen[branch] = true
	}
	return nil
}

// Check compares every configured branch and the latest release with the baseline.
func (client Client) Check(ctx context.Context, baseline Baseline) (Report, error) {
	if err := baseline.Validate(); err != nil {
		return Report{}, err
	}
	if client.HTTP == nil {
		return Report{}, errors.New("upstream watch requires an HTTP client")
	}
	if client.BaseURL == "" {
		client.BaseURL = "https://api.github.com"
	}

	report := Report{
		Repository:      baseline.Repository,
		ReviewedCommit:  baseline.ReviewedCommit,
		ReviewedRelease: baseline.ReviewedRelease,
	}

	release, err := client.latestRelease(ctx, baseline.Repository)
	if err != nil {
		return Report{}, err
	}
	report.LatestRelease = release
	if release.Tag != "" && release.Tag != baseline.ReviewedRelease {
		report.Reasons = append(report.Reasons, fmt.Sprintf("upstream release %s has not been reviewed", release.Tag))
	}

	for _, branch := range baseline.Branches {
		branchReport, err := client.checkBranch(ctx, baseline, branch)
		if err != nil {
			return Report{}, err
		}
		report.Branches = append(report.Branches, branchReport)
		if branchReport.Truncated {
			report.Reasons = append(report.Reasons, fmt.Sprintf("%s comparison reached GitHub's 300-file reporting limit", branch))
		}
		if branchReport.HeadCommit != baseline.ReviewedCommit && branchReport.Status != "ahead" {
			report.Reasons = append(report.Reasons, fmt.Sprintf("%s no longer has a simple history from the reviewed commit", branch))
		}
		if len(branchReport.Changes) > 0 {
			report.Reasons = append(report.Reasons, fmt.Sprintf("%s changed %d compatibility-sensitive file(s)", branch, len(branchReport.Changes)))
		}
	}

	report.ActionRequired = len(report.Reasons) > 0
	report.Markdown = RenderMarkdown(report)
	return report, nil
}

func (client Client) checkBranch(ctx context.Context, baseline Baseline, branch string) (BranchReport, error) {
	var current branchResponse
	endpoint := fmt.Sprintf("/repos/%s/branches/%s", baseline.Repository, url.PathEscape(branch))
	if err := client.getJSON(ctx, endpoint, &current); err != nil {
		return BranchReport{}, fmt.Errorf("read upstream branch %s: %w", branch, err)
	}
	if !commitPattern.MatchString(current.Commit.SHA) {
		return BranchReport{}, fmt.Errorf("upstream branch %s returned an invalid commit SHA", branch)
	}

	result := BranchReport{
		Name:       branch,
		HeadCommit: current.Commit.SHA,
		CompareURL: fmt.Sprintf("https://github.com/%s/compare/%s...%s", baseline.Repository, baseline.ReviewedCommit, current.Commit.SHA),
		Status:     "identical",
	}
	if current.Commit.SHA == baseline.ReviewedCommit {
		return result, nil
	}

	var comparison compareResponse
	comparePath := fmt.Sprintf("/repos/%s/compare/%s...%s?per_page=100", baseline.Repository, baseline.ReviewedCommit, current.Commit.SHA)
	if err := client.getJSON(ctx, comparePath, &comparison); err != nil {
		return BranchReport{}, fmt.Errorf("compare upstream branch %s: %w", branch, err)
	}
	result.Status = comparison.Status
	result.TotalCommits = comparison.TotalCommits
	result.Truncated = len(comparison.Files) >= 300
	for _, file := range comparison.Files {
		rules := classify(file.Filename)
		if len(rules) == 0 && file.PreviousFilename != "" {
			rules = classify(file.PreviousFilename)
		}
		if len(rules) == 0 {
			continue
		}
		result.Changes = append(result.Changes, Change{
			Path:         file.Filename,
			Status:       file.Status,
			PreviousPath: file.PreviousFilename,
			Rules:        rules,
		})
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		return result.Changes[i].Path < result.Changes[j].Path
	})
	return result, nil
}

func (client Client) latestRelease(ctx context.Context, repository string) (Release, error) {
	var response releaseResponse
	err := client.getJSON(ctx, fmt.Sprintf("/repos/%s/releases/latest", repository), &response)
	if errors.Is(err, errNotFound) {
		return Release{}, nil
	}
	if err != nil {
		return Release{}, fmt.Errorf("read latest upstream release: %w", err)
	}
	return Release{Tag: response.TagName, URL: response.HTMLURL}, nil
}

var errNotFound = errors.New("resource not found")

func (client Client) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(client.BaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "canopy-preflight-upstream-watch")
	if client.Token != "" {
		request.Header.Set("Authorization", "Bearer "+client.Token)
	}

	response, err := client.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("GitHub API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}

func classify(path string) []string {
	rules := make(map[string]bool)
	for _, rule := range exactSensitivePaths[path] {
		rules[rule] = true
	}
	if strings.HasPrefix(path, "lib/.proto/") {
		rules["CNPY003"] = true
	}
	if strings.HasPrefix(path, "plugin/") && (strings.HasSuffix(path, "/TUTORIAL.md") || strings.Contains(path, "/contract/")) {
		rules["CNPY001"] = true
		rules["CNPY002"] = true
		rules["CNPY003"] = true
	}
	result := make([]string, 0, len(rules))
	for rule := range rules {
		result = append(result, rule)
	}
	sort.Strings(result)
	return result
}

var exactSensitivePaths = map[string][]string{
	"fsm/state.go":                     {"CNPY002", "CNPY004"},
	"fsm/automatic.go":                 {"CNPY004"},
	"fsm/gov.go":                       {"CNPY004"},
	"fsm/gov_params.go":                {"CNPY004"},
	"fsm/transaction.go":               {"CNPY004"},
	"fsm/ethereum.go":                  {"CNPY003", "CNPY004"},
	"fsm/key.go":                       {"CNPY002", "CNPY004"},
	"lib/.proto/account.proto":         {"CNPY003", "CNPY004"},
	"lib/.proto/tx.proto":              {"CNPY003", "CNPY004"},
	"lib/.proto/plugin.proto":          {"CNPY001", "CNPY002", "CNPY003", "CNPY004"},
	"lib/plugin.go":                    {"CNPY001", "CNPY002", "CNPY003", "CNPY004"},
	"lib/codec.go":                     {"CNPY001", "CNPY003", "CNPY004"},
	"fsm/plugin_guard_test.go":         {"CNPY002"},
	"plugin/go/TUTORIAL.md":            {"CNPY001", "CNPY002"},
	"plugin/go/contract/contract.go":   {"CNPY001", "CNPY002", "CNPY003"},
	"plugin/go/contract/plugin.go":     {"CNPY001", "CNPY002", "CNPY003"},
	"plugin/go/contract/account.pb.go": {"CNPY003"},
}

// RenderMarkdown returns the stable body used by the rolling review issue.
func RenderMarkdown(report Report) string {
	var output strings.Builder
	fmt.Fprintln(&output, IssueMarker)
	fmt.Fprintln(&output, "# Canopy upstream compatibility review")
	fmt.Fprintln(&output)
	fmt.Fprintf(&output, "Reviewed commit: [`%s`](https://github.com/%s/commit/%s)\n\n", report.ReviewedCommit, report.Repository, report.ReviewedCommit)
	if report.LatestRelease.Tag != "" {
		fmt.Fprintf(&output, "Latest release: [%s](%s)\n\n", report.LatestRelease.Tag, report.LatestRelease.URL)
	}
	if len(report.Reasons) > 0 {
		fmt.Fprintln(&output, "## Why review is required")
		fmt.Fprintln(&output)
		for _, reason := range report.Reasons {
			fmt.Fprintf(&output, "- %s\n", reason)
		}
		fmt.Fprintln(&output)
	}
	for _, branch := range report.Branches {
		fmt.Fprintf(&output, "## `%s`\n\n", branch.Name)
		fmt.Fprintf(&output, "Head: [`%s`](https://github.com/%s/commit/%s) · [compare](%s) · %d commit(s)\n\n", branch.HeadCommit, report.Repository, branch.HeadCommit, branch.CompareURL, branch.TotalCommits)
		if len(branch.Changes) == 0 {
			fmt.Fprintln(&output, "No monitored compatibility files changed.")
			fmt.Fprintln(&output)
			continue
		}
		fmt.Fprintln(&output, "| File | Status | Rules to review |")
		fmt.Fprintln(&output, "| --- | --- | --- |")
		for _, change := range branch.Changes {
			fmt.Fprintf(&output, "| `%s` | %s | %s |\n", change.Path, change.Status, strings.Join(change.Rules, ", "))
		}
		fmt.Fprintln(&output)
	}
	fmt.Fprintln(&output, "## Required action")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "1. Inspect the linked upstream diff.")
	fmt.Fprintln(&output, "2. Update affected rule contracts and fixtures if behavior changed.")
	fmt.Fprintln(&output, "3. Run `make check` and test against the official Canopy plugin.")
	fmt.Fprintln(&output, "4. Update `docs/upstream-baseline.json` only through a reviewed pull request.")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "Do not update the baseline merely to dismiss this issue.")
	return output.String()
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/jerrygeorge360/canopy-preflight/internal/upstreamwatch"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "upstream-watch:", err)
		os.Exit(1)
	}
}

func run() error {
	baselinePath := flag.String("baseline", "docs/upstream-baseline.json", "reviewed upstream baseline")
	outputPath := flag.String("output", "upstream-report.json", "JSON report destination")
	flag.Parse()

	baseline, err := upstreamwatch.LoadBaseline(*baselinePath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	report, err := (upstreamwatch.Client{
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		BaseURL: os.Getenv("UPSTREAM_API_URL"),
		Token:   os.Getenv("GITHUB_TOKEN"),
	}).Check(ctx, baseline)
	if err != nil {
		return err
	}

	contents, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode upstream report: %w", err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(*outputPath, contents, 0o600); err != nil {
		return fmt.Errorf("write upstream report: %w", err)
	}
	if report.ActionRequired {
		fmt.Println("Canopy upstream review required")
	} else {
		fmt.Println("Canopy upstream baseline is current")
	}
	return nil
}

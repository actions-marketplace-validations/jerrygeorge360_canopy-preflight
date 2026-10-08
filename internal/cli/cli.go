// Package cli implements argument parsing and command orchestration.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jerrygeorge360/canopy-preflight/internal/diagnostic"
	"github.com/jerrygeorge360/canopy-preflight/internal/evidence"
	"github.com/jerrygeorge360/canopy-preflight/internal/project"
	"github.com/jerrygeorge360/canopy-preflight/internal/report"
	"github.com/jerrygeorge360/canopy-preflight/internal/rules"
	"github.com/jerrygeorge360/canopy-preflight/internal/upstream"
)

const usage = `usage:
  canopy-doctor check [--format human|json] [--upstream-base SHA --upstream-target SHA] [--deployment-height N --activation-height N --required-protocol-version N] [path]
  canopy-doctor version
  canopy-doctor --version
  canopy-doctor --help`

// Run executes the CLI and returns its process exit code.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	return guardedRun(stderr, func() int {
		return run(args, stdout, stderr, version)
	})
}

func guardedRun(stderr io.Writer, execute func() int) (exitCode int) {
	defer func() {
		if recover() != nil {
			fmt.Fprintln(stderr, "error: internal inspection failure")
			exitCode = 1
		}
	}()
	return execute()
}

func run(args []string, stdout, stderr io.Writer, version string) int {
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version" || args[0] == "-v") {
		fmt.Fprintf(stdout, "canopy-doctor %s\n", version)
		return 0
	}
	options, err := parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %s\n%s\n", err, usage)
		return 1
	}

	target, err := project.Validate(options.path)
	if err != nil {
		fmt.Fprintf(stderr, "error: %s\n", err)
		return 1
	}
	var drift *upstream.Snapshot
	if options.upstreamBase != "" {
		snapshot, err := upstream.Inspect(target.Path, options.upstreamBase, options.upstreamTarget)
		if err != nil {
			fmt.Fprintf(stderr, "error: upstream comparison unavailable: %s\n", err)
			return 1
		}
		drift = &snapshot
	}

	collected, err := evidence.Collect(target.Path)
	if err != nil {
		fmt.Fprintln(stderr, "error: could not inspect target")
		return 1
	}
	findings := rules.Evaluate(collected)
	if drift != nil {
		findings = append(findings, rules.EvaluateForkDrift(*drift, options.releaseContext())...)
	}
	result, err := report.New(version, target.Display, findings)
	if err != nil {
		fmt.Fprintln(stderr, "error: could not create report")
		return 1
	}
	if options.format == "json" {
		err = report.WriteJSON(stdout, result)
	} else {
		err = report.WriteHuman(stdout, result)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error: could not write report")
		return 1
	}
	return diagnostic.ExitCode(result.Decision)
}

type options struct {
	format string
	path   string

	upstreamBase   string
	upstreamTarget string

	deploymentHeight        uint64
	activationHeight        uint64
	requiredProtocolVersion uint64
	hasReleaseContext       bool
}

func (value options) releaseContext() rules.ReleaseContext {
	return rules.ReleaseContext{
		Supplied:                value.hasReleaseContext,
		DeploymentHeight:        value.deploymentHeight,
		ActivationHeight:        value.activationHeight,
		RequiredProtocolVersion: value.requiredProtocolVersion,
	}
}

func parse(args []string) (options, error) {
	if len(args) == 0 || args[0] != "check" {
		return options{}, errors.New("expected the check command")
	}
	result := options{format: "human", path: "."}
	pathSet := false
	formatSet := false
	upstreamBaseSet := false
	upstreamTargetSet := false
	deploymentSet := false
	activationSet := false
	protocolSet := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--format":
			if formatSet {
				return options{}, errors.New("--format may be provided only once")
			}
			if i+1 >= len(args) {
				return options{}, errors.New("--format requires a value")
			}
			i++
			result.format = args[i]
			formatSet = true
		case strings.HasPrefix(arg, "--format="):
			if formatSet {
				return options{}, errors.New("--format may be provided only once")
			}
			result.format = strings.TrimPrefix(arg, "--format=")
			formatSet = true
		case arg == "--upstream-base" || arg == "--upstream-target" || arg == "--deployment-height" || arg == "--activation-height" || arg == "--required-protocol-version":
			if i+1 >= len(args) {
				return options{}, fmt.Errorf("%s requires a value", arg)
			}
			i++
			if err := setOption(&result, arg, args[i], &upstreamBaseSet, &upstreamTargetSet, &deploymentSet, &activationSet, &protocolSet); err != nil {
				return options{}, err
			}
		case strings.HasPrefix(arg, "--upstream-base="):
			if err := setOption(&result, "--upstream-base", strings.TrimPrefix(arg, "--upstream-base="), &upstreamBaseSet, &upstreamTargetSet, &deploymentSet, &activationSet, &protocolSet); err != nil {
				return options{}, err
			}
		case strings.HasPrefix(arg, "--upstream-target="):
			if err := setOption(&result, "--upstream-target", strings.TrimPrefix(arg, "--upstream-target="), &upstreamBaseSet, &upstreamTargetSet, &deploymentSet, &activationSet, &protocolSet); err != nil {
				return options{}, err
			}
		case strings.HasPrefix(arg, "--deployment-height="):
			if err := setOption(&result, "--deployment-height", strings.TrimPrefix(arg, "--deployment-height="), &upstreamBaseSet, &upstreamTargetSet, &deploymentSet, &activationSet, &protocolSet); err != nil {
				return options{}, err
			}
		case strings.HasPrefix(arg, "--activation-height="):
			if err := setOption(&result, "--activation-height", strings.TrimPrefix(arg, "--activation-height="), &upstreamBaseSet, &upstreamTargetSet, &deploymentSet, &activationSet, &protocolSet); err != nil {
				return options{}, err
			}
		case strings.HasPrefix(arg, "--required-protocol-version="):
			if err := setOption(&result, "--required-protocol-version", strings.TrimPrefix(arg, "--required-protocol-version="), &upstreamBaseSet, &upstreamTargetSet, &deploymentSet, &activationSet, &protocolSet); err != nil {
				return options{}, err
			}
		case strings.HasPrefix(arg, "-"):
			return options{}, fmt.Errorf("unknown option %q", arg)
		default:
			if pathSet {
				return options{}, errors.New("only one project path may be provided")
			}
			result.path = arg
			pathSet = true
		}
	}
	if result.format != "human" && result.format != "json" {
		return options{}, fmt.Errorf("unsupported format %q", result.format)
	}
	if upstreamBaseSet != upstreamTargetSet {
		return options{}, errors.New("--upstream-base and --upstream-target must be supplied together")
	}
	if (deploymentSet || activationSet || protocolSet) && !(deploymentSet && activationSet && protocolSet) {
		return options{}, errors.New("--deployment-height, --activation-height, and --required-protocol-version must be supplied together")
	}
	if deploymentSet && !upstreamBaseSet {
		return options{}, errors.New("release context requires --upstream-base and --upstream-target")
	}
	result.hasReleaseContext = deploymentSet && activationSet && protocolSet
	return result, nil
}

func setOption(result *options, name, value string, upstreamBaseSet, upstreamTargetSet, deploymentSet, activationSet, protocolSet *bool) error {
	switch name {
	case "--upstream-base":
		if *upstreamBaseSet {
			return errors.New("--upstream-base may be provided only once")
		}
		if !fullSHA(value) {
			return errors.New("--upstream-base must be a full 40-character hexadecimal commit SHA")
		}
		result.upstreamBase = strings.ToLower(value)
		*upstreamBaseSet = true
	case "--upstream-target":
		if *upstreamTargetSet {
			return errors.New("--upstream-target may be provided only once")
		}
		if !fullSHA(value) {
			return errors.New("--upstream-target must be a full 40-character hexadecimal commit SHA")
		}
		result.upstreamTarget = strings.ToLower(value)
		*upstreamTargetSet = true
	case "--deployment-height":
		if *deploymentSet {
			return errors.New("--deployment-height may be provided only once")
		}
		parsed, err := decimalUint(value)
		if err != nil {
			return errors.New("--deployment-height must be an unsigned decimal integer")
		}
		result.deploymentHeight = parsed
		*deploymentSet = true
	case "--activation-height":
		if *activationSet {
			return errors.New("--activation-height may be provided only once")
		}
		parsed, err := decimalUint(value)
		if err != nil {
			return errors.New("--activation-height must be an unsigned decimal integer")
		}
		result.activationHeight = parsed
		*activationSet = true
	case "--required-protocol-version":
		if *protocolSet {
			return errors.New("--required-protocol-version may be provided only once")
		}
		parsed, err := decimalUint(value)
		if err != nil {
			return errors.New("--required-protocol-version must be an unsigned decimal integer")
		}
		result.requiredProtocolVersion = parsed
		*protocolSet = true
	default:
		return fmt.Errorf("unknown option %q", name)
	}
	return nil
}

func decimalUint(value string) (uint64, error) {
	if value == "" {
		return 0, errors.New("empty")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errors.New("not decimal")
		}
	}
	return strconv.ParseUint(value, 10, 64)
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

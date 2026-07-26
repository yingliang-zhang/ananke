// Command ananke-trusted-supervisor-release builds, publishes, and verifies the
// exact production trusted-supervisor artifact selected by an operator.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/yingliang-zhang/ananke/internal/releaseartifact"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ananke-trusted-supervisor-release: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("exactly one mode is required: build or verify")
	}
	switch arguments[0] {
	case "build":
		values, err := parseClosedArguments(arguments[1:], "--repository-root", "--output")
		if err != nil {
			return fmt.Errorf("build arguments: %w", err)
		}
		if err := releaseartifact.BuildAndPublishTrustedSupervisor(ctx, values["--repository-root"], values["--output"], os.Environ()); err != nil {
			return err
		}
		fmt.Fprintf(output, "published and verified exact artifact: %s\n", values["--output"])
		return nil
	case "verify":
		values, err := parseClosedArguments(arguments[1:], "--artifact")
		if err != nil {
			return fmt.Errorf("verify arguments: %w", err)
		}
		if err := releaseartifact.VerifyTrustedSupervisor(values["--artifact"]); err != nil {
			return err
		}
		fmt.Fprintf(output, "verified exact artifact: %s\n", values["--artifact"])
		return nil
	default:
		return fmt.Errorf("unknown mode %q; want build or verify", arguments[0])
	}
}

func parseClosedArguments(arguments []string, names ...string) (map[string]string, error) {
	if len(arguments) != len(names)*2 {
		return nil, fmt.Errorf("require exactly %s", joinArgumentNames(names))
	}
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	values := make(map[string]string, len(names))
	for index := 0; index < len(arguments); index += 2 {
		name, value := arguments[index], arguments[index+1]
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unknown argument %q", name)
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf("duplicate argument %q", name)
		}
		if value == "" {
			return nil, fmt.Errorf("argument %q requires a nonempty value", name)
		}
		values[name] = value
	}
	for _, name := range names {
		if _, ok := values[name]; !ok {
			return nil, fmt.Errorf("missing argument %q", name)
		}
	}
	return values, nil
}

func joinArgumentNames(names []string) string {
	result := ""
	for index, name := range names {
		if index > 0 {
			result += " and "
		}
		result += name + " PATH"
	}
	return result
}

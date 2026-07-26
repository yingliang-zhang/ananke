package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestProductionCommandRequiresRepositoryAndExecutionPolicyFlags(t *testing.T) {
	arguments := []string{
		"--socket", "/tmp/trusted-supervisor.sock",
		"--trust-bundle", "/tmp/trust-bundle.json",
		"--private-key-bundle", "/tmp/private-key-bundle.json",
		"--journal", "/tmp/server-journal.sqlite",
		"--expected-client-uid", strconv.Itoa(os.Getuid()),
	}
	if err := run(context.Background(), arguments); err == nil || !strings.Contains(err.Error(), "--repository-policy") ||
		!strings.Contains(err.Error(), "--execution-policy") {
		t.Fatalf("missing policy flags error = %v", err)
	}
	arguments = append(arguments, "--repository-policy", "/tmp/repository-policy.json")
	if err := run(context.Background(), arguments); err == nil || !strings.Contains(err.Error(), "--execution-policy") {
		t.Fatalf("missing execution policy error = %v", err)
	}
	arguments = append(arguments, "--execution-policy", "/tmp/execution-policy.json")
	if err := run(context.Background(), arguments); err == nil || !strings.Contains(err.Error(), "--runtime-uid") {
		t.Fatalf("missing runtime credential flags error = %v", err)
	}
	arguments = append(arguments, "--runtime-uid", "501", "--runtime-gid", "20")
	if err := run(context.Background(), arguments); err == nil || strings.Contains(err.Error(), "are required") {
		t.Fatalf("provided authority flags were not accepted by parsing: %v", err)
	}
}

package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestProductionCommandRequiresRepositoryPolicyFlag(t *testing.T) {
	arguments := []string{
		"--socket", "/tmp/trusted-supervisor.sock",
		"--trust-bundle", "/tmp/trust-bundle.json",
		"--private-key-bundle", "/tmp/private-key-bundle.json",
		"--journal", "/tmp/server-journal.sqlite",
		"--expected-client-uid", strconv.Itoa(os.Getuid()),
	}
	if err := run(context.Background(), arguments); err == nil || !strings.Contains(err.Error(), "--repository-policy") {
		t.Fatalf("missing repository policy error = %v", err)
	}
	arguments = append(arguments, "--repository-policy", "/tmp/repository-policy.json")
	if err := run(context.Background(), arguments); err == nil || strings.Contains(err.Error(), "are required") {
		t.Fatalf("provided repository policy flag was not accepted by parsing: %v", err)
	}
}

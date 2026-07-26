//go:build !ananke_test_runtime_authority

package main

import "github.com/yingliang-zhang/ananke/internal/trustedsupervisor"

func newTrustedSupervisorServer(config trustedsupervisor.ServerConfig) (*trustedsupervisor.Server, error) {
	return trustedsupervisor.NewServer(config)
}

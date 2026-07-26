//go:build !darwin

package releaseartifact

import "errors"

func newBuildLaunchMutationGuard(compilerFD, compilerParentFD, repositoryFD, repositoryParentFD int) (buildLaunchMutationGuard, error) {
	return nil, errors.New("build launch mutation proof is unsupported on this platform")
}

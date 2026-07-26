//go:build !darwin

package releaseartifact

import "errors"

func renameNoReplace(sourceDirectoryFD int, sourceName string, destinationDirectoryFD int, destinationName string) error {
	return errors.New("atomic no-replace release publication is unsupported on this platform")
}

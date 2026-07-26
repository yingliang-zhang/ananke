//go:build darwin

package releaseartifact

import "golang.org/x/sys/unix"

func renameNoReplace(sourceDirectoryFD int, sourceName string, destinationDirectoryFD int, destinationName string) error {
	return unix.RenameatxNp(sourceDirectoryFD, sourceName, destinationDirectoryFD, destinationName, unix.RENAME_EXCL)
}

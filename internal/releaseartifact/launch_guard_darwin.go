//go:build darwin

package releaseartifact

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type darwinVnodeFingerprint struct {
	mode        uint16
	uid         uint32
	gid         uint32
	flags       uint32
	links       uint64
	size        int64
	modifiedSec int64
	modifiedNS  int64
	changedSec  int64
	changedNS   int64
}

type darwinBuildLaunchMutationGuard struct {
	queue        int
	labels       map[uint64]string
	fingerprints map[uint64]darwinVnodeFingerprint
	mutated      []string
}

func newBuildLaunchMutationGuard(compilerFD, compilerParentFD, repositoryFD, repositoryParentFD int) (buildLaunchMutationGuard, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("create Darwin build launch mutation queue: %w", err)
	}
	guard := &darwinBuildLaunchMutationGuard{
		queue: queue, labels: make(map[uint64]string), fingerprints: make(map[uint64]darwinVnodeFingerprint),
	}
	watched := []struct {
		fd    int
		label string
	}{
		{fd: compilerFD, label: "compiler object"},
		{fd: compilerParentFD, label: "compiler parent directory"},
		{fd: repositoryFD, label: "repository object"},
		{fd: repositoryParentFD, label: "repository parent directory"},
	}
	changes := make([]unix.Kevent_t, 0, len(watched))
	for _, watch := range watched {
		identifier := uint64(watch.fd)
		if prior, exists := guard.labels[identifier]; exists {
			guard.labels[identifier] = prior + "/" + watch.label
			continue
		}
		guard.labels[identifier] = watch.label
		fingerprint, err := readDarwinVnodeFingerprint(watch.fd)
		if err != nil {
			unix.Close(queue)
			return nil, fmt.Errorf("capture %s mutation baseline: %w", watch.label, err)
		}
		guard.fingerprints[identifier] = fingerprint
		var change unix.Kevent_t
		unix.SetKevent(&change, watch.fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_ENABLE|unix.EV_CLEAR)
		change.Fflags = unix.NOTE_WRITE | unix.NOTE_DELETE | unix.NOTE_EXTEND | unix.NOTE_ATTRIB | unix.NOTE_LINK | unix.NOTE_RENAME | unix.NOTE_REVOKE
		changes = append(changes, change)
	}
	if _, err := unix.Kevent(queue, changes, nil, nil); err != nil {
		unix.Close(queue)
		return nil, fmt.Errorf("register Darwin vnode build launch mutation watches: %w", err)
	}
	return guard, nil
}

func (guard *darwinBuildLaunchMutationGuard) Check() error {
	if len(guard.mutated) != 0 {
		return fmt.Errorf("build launch mutation observed: %s", strings.Join(guard.mutated, ", "))
	}
	events := make([]unix.Kevent_t, len(guard.labels))
	timeout := unix.Timespec{}
	n, err := unix.Kevent(guard.queue, nil, events, &timeout)
	if err != nil {
		return fmt.Errorf("read Darwin build launch mutation queue: %w", err)
	}
	for _, event := range events[:n] {
		label := guard.labels[event.Ident]
		if label == "" {
			label = fmt.Sprintf("descriptor %d", event.Ident)
		}
		if event.Flags&unix.EV_ERROR != 0 && event.Data != 0 {
			guard.mutated = append(guard.mutated, fmt.Sprintf("%s watch error %d", label, event.Data))
			continue
		}
		if event.Fflags == unix.NOTE_ATTRIB {
			current, err := readDarwinVnodeFingerprint(int(event.Ident))
			if err == nil && current == guard.fingerprints[event.Ident] {
				continue
			}
		}
		guard.mutated = append(guard.mutated, fmt.Sprintf("%s flags %#x", label, event.Fflags))
	}
	if len(guard.mutated) == 0 {
		return nil
	}
	sort.Strings(guard.mutated)
	return fmt.Errorf("build launch mutation observed: %s", strings.Join(guard.mutated, ", "))
}

func readDarwinVnodeFingerprint(fd int) (darwinVnodeFingerprint, error) {
	var status unix.Stat_t
	if err := unix.Fstat(fd, &status); err != nil {
		return darwinVnodeFingerprint{}, err
	}
	return darwinVnodeFingerprint{
		mode: uint16(status.Mode), uid: status.Uid, gid: status.Gid, flags: status.Flags,
		links: uint64(status.Nlink), size: status.Size,
		modifiedSec: status.Mtim.Sec, modifiedNS: status.Mtim.Nsec,
		changedSec: status.Ctim.Sec, changedNS: status.Ctim.Nsec,
	}, nil
}

func (guard *darwinBuildLaunchMutationGuard) Close() error {
	if guard.queue < 0 {
		return nil
	}
	err := unix.Close(guard.queue)
	guard.queue = -1
	if errors.Is(err, unix.EBADF) {
		return nil
	}
	return err
}

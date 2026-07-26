//go:build darwin

package trustedsupervisor

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinNamespaceAdmissionRejectsEveryExtendedACLAuthority(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	group, err := user.LookupGroupId(current.Gid)
	if err != nil {
		t.Fatal(err)
	}
	permissions := "add_file,add_subdirectory,delete_child,writeattr,writeextattr"
	cases := []struct {
		name     string
		entry    string
		ancestor bool
	}{
		{name: "named user on configured root", entry: "user:" + current.Username + " allow " + permissions},
		{name: "named group on physical ancestor", entry: "group:" + group.Name + " allow " + permissions, ancestor: true},
		{name: "everyone on configured root", entry: "everyone allow " + permissions},
		{name: "inherited everyone on physical ancestor", entry: "everyone allow " + permissions + ",file_inherit,directory_inherit", ancestor: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			container := t.TempDir()
			ancestor := filepath.Join(container, "ancestor")
			root := filepath.Join(ancestor, "root")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			target := root
			if testCase.ancestor {
				target = ancestor
			}
			installDarwinACLForTest(t, target, testCase.entry)
			assertDarwinDescriptorHasRejectedACLForTest(t, target)

			options := testAuditNamespaceAuthorityOptions()
			options.testOnlyStable = false
			authority, err := openAuditNamespaceAuthority([]string{root}, options)
			if authority != nil {
				_ = authority.Close()
			}
			var typed *InvocationNamespaceError
			if !errors.As(err, &typed) || typed.Reason != InvocationNamespaceWritable {
				t.Fatalf("ACL hierarchy admission = %v, want %q", err, InvocationNamespaceWritable)
			}
		})
	}
}

func TestDarwinACLAuthorizedRenameWithoutDecoyRemainsNonterminalLiveAndRestart(t *testing.T) {
	for _, recovery := range []string{"live", "restart"} {
		t.Run(recovery, func(t *testing.T) {
			assertFinalizingRenameWithoutDecoyRemainsNonterminal(t, recovery, func(t *testing.T, _ *auditExecutionHistoryFixture, target auditOwnedRootIdentity) {
				installDarwinACLForTest(t, target.ParentPath, "everyone allow add_file,add_subdirectory,delete_child,writeattr,writeextattr")
				assertDarwinDescriptorHasRejectedACLForTest(t, target.ParentPath)
			})
		})
	}
}

func installDarwinACLForTest(t *testing.T, path, entry string) {
	t.Helper()
	command := exec.Command("/bin/chmod", "+a", entry, path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("native Darwin ACL creation unavailable: %v: %s", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("/bin/chmod", "-N", path).Run()
	})
}

func assertDarwinDescriptorHasRejectedACLForTest(t *testing.T, path string) {
	t.Helper()
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(descriptor)
	inspection, probeErr := inspectAuditNamespaceACL(descriptor)
	if err := validateAuditNamespaceACLInspection(inspection, probeErr); err == nil {
		t.Fatalf("native Darwin ACL on %q was accepted: %+v", path, inspection)
	}
	if !inspection.Supported || !inspection.Nontrivial || probeErr != nil {
		t.Fatalf("native Darwin ACL probe = %+v, %v", inspection, probeErr)
	}
}

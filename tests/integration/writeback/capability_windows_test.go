package writeback_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"golang.org/x/sys/windows"
)

func TestCapabilityInspectsActualRootWithoutWriting(t *testing.T) {
	root, original, policy, request, _, _ := setup(t)
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := writeback.InspectCapability(policy.Roots[0])
	if err != nil || !capability.Available || capability.Replacement != "recoverableReplace" || len(capability.Formats) != 1 || capability.Formats[0] != "flac" {
		t.Fatalf("capability: %+v %v", capability, err)
	}
	wrong := policy.Roots[0]
	wrong.Identity = "a-different-root"
	if _, err = writeback.InspectCapability(wrong); err == nil {
		t.Fatal("root identity was not checked")
	}
	wrong = policy.Roots[0]
	wrong.Path = `\\server\share\music`
	if _, err = writeback.InspectCapability(wrong); err == nil {
		t.Fatal("network root advertised writeback")
	}
	after, err := os.ReadDir(root)
	if err != nil || len(before) != len(after) {
		t.Fatal("capability check modified the root")
	}
	for i := range before {
		if before[i].Name() != after[i].Name() {
			t.Fatal("capability check changed directory entries")
		}
	}
	current, err := os.ReadFile(filepath.Join(root, request.Target.RelativePath))
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("capability check modified source content")
	}
}

func TestCapabilityAcceptsModifyACLWithFileDeletePermission(t *testing.T) {
	root, _, policy, request, h, journal := setup(t)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	// Standard Modify grants file DELETE and directory ADD_FILE but not
	// directory DELETE_CHILD. It must permit the actual protected renames.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;0x1301bf;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	// ACL propagation changes the file change-time, so capture the authorized
	// source version after setting the fixture's permissions.
	policy, request, h = requestFor(t, root, request.Target.RelativePath)
	name, _ := windows.UTF16PtrFromString(root)
	file, err := windows.CreateFile(name, 0x40, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err == nil {
		windows.CloseHandle(file)
		t.Fatal("fixture unexpectedly permits directory DELETE_CHILD")
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("expected ACL denial, not a sharing conflict", err)
	}
	if _, err = writeback.InspectCapability(policy.Roots[0]); err != nil {
		t.Fatal("ordinary Modify ACL rejected", err)
	}
	result, err := service(t, policy, journal, h).Apply(context.Background(), "admin", request)
	if err != nil || !result.Changed {
		t.Fatal("file DELETE authorization did not permit recoverable replacement", err)
	}
}

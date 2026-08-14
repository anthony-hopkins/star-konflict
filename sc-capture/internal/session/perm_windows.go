//go:build windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no file mode worth the name: os.Chmod there only toggles the
// read-only attribute, which stops nothing. A session contains login
// credentials in the clear, so Principle VIII needs real enforcement, and on
// this platform that means an explicit DACL.
//
// The ACL installed grants full control to the owning user and to SYSTEM, and
// to nobody else. SYSTEM is kept deliberately: excluding it breaks backup,
// indexing and antivirus in ways that get the tool uninstalled rather than
// making anyone safer, and a machine's SYSTEM account can read the file
// regardless of what any ACL says.
//
// Inheritance is switched off. Without that, a session written under a
// directory whose ACL grants Users read access stays readable by every account
// on the machine no matter what we add.

// secureDir installs an owner-only ACL on a session directory.
func secureDir(path string) error { return secureNamed(path, true) }

func secureNamed(path string, isDir bool) error {
	user, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("resolving current user: %w", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("resolving SYSTEM: %w", err)
	}

	// Directories need the inherit flags so that files created inside them are
	// protected too; files themselves take no inheritance.
	var inherit uint32
	if isDir {
		inherit = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	} else {
		inherit = windows.NO_INHERITANCE
	}

	access := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inherit,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(user),
			},
		},
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inherit,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(system),
			},
		},
	}

	acl, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return fmt.Errorf("building ACL: %w", err)
	}

	// PROTECTED_DACL_SECURITY_INFORMATION is what severs inheritance. Without
	// it the entries above are added to whatever the parent already granted,
	// which is usually the whole Users group.
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	)
}

func currentUserSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	u, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid, nil
}

// inspectPermissions reports who can reach the bundle.
//
// Anything granted to a trustee other than the owner or SYSTEM is reported as
// loose. Unlike the Unix path this cannot be reduced to a mode string, so the
// summary names the principals instead — a reader should not have to guess
// what protection a Windows bundle actually had.
func inspectPermissions(dir string) (summary string, loose []string, err error) {
	self, err := currentUserSID()
	if err != nil {
		return "", nil, err
	}

	check := func(path, label string) error {
		others, err := nonOwnerTrustees(path, self)
		if err != nil {
			return err
		}
		if len(others) > 0 {
			loose = append(loose, fmt.Sprintf("%s (%s)", label, strings.Join(others, ", ")))
		}
		return nil
	}

	if err := check(dir, filepath.Base(dir)+"\\"); err != nil {
		return "", nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "owner-only ACL", loose, err
	}
	for _, e := range entries {
		if err := check(filepath.Join(dir, e.Name()), e.Name()); err != nil {
			continue
		}
	}

	if len(loose) == 0 {
		summary = "owner-only ACL (owner + SYSTEM)"
	} else {
		summary = "ACL grants access beyond the owner"
	}
	return summary, loose, nil
}

// nonOwnerTrustees lists principals with access other than the owner and
// SYSTEM.
func nonOwnerTrustees(path string, self *windows.SID) ([]string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nil, err
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		// A nil DACL means "no protection at all", which is worse than a
		// permissive one and must not read as clean.
		return []string{"everyone (no DACL present)"}, nil
	}

	var out []string
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue // deny entries do not widen access
		}
		// The SID is not a pointer field: it is laid out inline at the end of
		// the ACE, and SidStart is its first word.
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid.Equals(self) || sid.IsWellKnown(windows.WinLocalSystemSid) {
			continue
		}
		out = append(out, sidLabel(sid))
	}
	return out, nil
}

func sidLabel(sid *windows.SID) string {
	if account, domain, _, err := sid.LookupAccount(""); err == nil {
		if domain != "" {
			return domain + "\\" + account
		}
		return account
	}
	return sid.String()
}

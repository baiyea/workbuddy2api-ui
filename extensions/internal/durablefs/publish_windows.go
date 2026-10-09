package durablefs

import "golang.org/x/sys/windows"

// Windows cannot fsync a directory opened by os.Open. Publish with the native
// write-through rename instead; do not remove the destination first. ACLs are
// inherited from the user's application-data directory (POSIX modes do not
// configure Windows ACLs).
func Publish(source, target string, replace bool) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	return windows.MoveFileEx(from, to, flags)
}

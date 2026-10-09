package bundle

import "fmt"

// UnsupportedVersionError reports a bundle written in a format version this
// build does not understand.
type UnsupportedVersionError struct {
	Found string
}

// Error describes the version found and the one this build reads.
func (r *UnsupportedVersionError) Error() string {
	return fmt.Sprintf(
		"bundle format version %q is not supported, this build of quo reads %q",
		r.Found,
		FormatVersion)
}

// Is reports whether err is an UnsupportedVersionError.
func (r *UnsupportedVersionError) Is(err error) (matched bool) {
	_, matched = err.(*UnsupportedVersionError)
	return
}

// DigestMismatchError reports a member whose content does not match the digest
// the manifest records for it.
type DigestMismatchError struct {
	Path string
	Want string
	Got  string
}

// Error names the member and both digests.
func (r *DigestMismatchError) Error() string {
	return fmt.Sprintf(
		"member %q does not match its digest: manifest records %s, content is %s",
		r.Path,
		r.Want,
		r.Got)
}

// Is reports whether err is a DigestMismatchError.
func (r *DigestMismatchError) Is(err error) (matched bool) {
	_, matched = err.(*DigestMismatchError)
	return
}

// FormatError reports a file that is not shaped like a bundle.
type FormatError struct {
	Reason string
}

// Error describes how the file departs from the bundle format.
func (r *FormatError) Error() string {
	return "not a valid bundle: " + r.Reason
}

// Is reports whether err is a FormatError.
func (r *FormatError) Is(err error) (matched bool) {
	_, matched = err.(*FormatError)
	return
}

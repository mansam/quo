package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// digest returns the SHA-256 digest of content, prefixed with the algorithm
// that produced it so that a reader never has to guess.
func digest(content []byte) (sum string) {
	hashed := sha256.New()
	_, _ = hashed.Write(content)
	sum = encodeDigest(hashed)
	return
}

// digestStream returns the SHA-256 digest of everything readable from in,
// without holding it in memory. A disk member is far too large to buffer.
func digestStream(in io.Reader) (sum string, err error) {
	hashed := sha256.New()
	_, err = io.Copy(hashed, in)
	if err != nil {
		return
	}
	sum = encodeDigest(hashed)
	return
}

// copyDigest copies everything readable from in to out, returning the SHA-256
// digest of what passed through and how much of it there was. A disk is
// hashed as it is written so that it is read from local disk only once.
func copyDigest(out io.Writer, in io.Reader) (sum string, written int64, err error) {
	hashed := sha256.New()
	written, err = io.Copy(io.MultiWriter(out, hashed), in)
	if err != nil {
		return
	}
	sum = encodeDigest(hashed)
	return
}

// encodeDigest renders a computed hash as an algorithm-prefixed digest.
func encodeDigest(hashed hash.Hash) (sum string) {
	sum = "sha256:" + hex.EncodeToString(hashed.Sum(nil))
	return
}

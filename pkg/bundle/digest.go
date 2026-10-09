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

// encodeDigest renders a computed hash as an algorithm-prefixed digest.
func encodeDigest(hashed hash.Hash) (sum string) {
	sum = "sha256:" + hex.EncodeToString(hashed.Sum(nil))
	return
}

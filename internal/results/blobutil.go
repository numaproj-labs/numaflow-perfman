package results

import (
	"crypto/sha256"
	"encoding/hex"
)

func blobStats(content []byte) (sizeBytes int64, sha256Hex string) {
	sizeBytes = int64(len(content))
	sum := sha256.Sum256(content)
	sha256Hex = hex.EncodeToString(sum[:])
	return sizeBytes, sha256Hex
}

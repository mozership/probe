package smtp

import (
	"crypto/rand"
	"encoding/hex"
)

// randomID returns 128 random bits in hex, for a Message-ID to be unique.
func randomID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

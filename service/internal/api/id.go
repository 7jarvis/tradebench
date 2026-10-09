package api

import (
	"crypto/rand"
	"encoding/hex"
)

func randomID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

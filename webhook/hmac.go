package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

func ValidateHMAC(body []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	gotHex := signature[len("sha256="):]
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return false
	}
	want := hmac.New(sha256.New, []byte(secret))
	want.Write(body)
	return subtle.ConstantTimeCompare(got, want.Sum(nil)) == 1
}

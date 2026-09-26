package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Proof is appsecret_proof: the hex HMAC-SHA256 of the access token, keyed
// with the app secret. With "Require App Secret" switched on in the app,
// Meta refuses every call without it, so a leaked token alone is useless.
func Proof(token, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(token))
	return hex.EncodeToString(m.Sum(nil))
}

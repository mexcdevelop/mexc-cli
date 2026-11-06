package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HMACSHA256Hex is a standalone HMAC-SHA256 digest primitive: keyed by secret,
// over payload, lower-case hex. It exists alongside the Signer port for a call
// site that needs the raw digest but has no RequestSpec to hand to a Signer —
// e.g. a WS-backed operation's in-connection login frame (accessKey+reqTime,
// not a query string) or a one-shot preflight REST call a WSCollect closure
// describes as data rather than sends itself. Every scheme-specific Signer
// implementation is free to keep computing its own digest inline (as the
// existing cli-hmac adapter does); this is a separate, narrower primitive for
// call sites outside that machinery, not a replacement for it.
func HMACSHA256Hex(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

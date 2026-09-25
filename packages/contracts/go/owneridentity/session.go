package owneridentity

import (
	"crypto/sha256"
	"encoding/hex"
)

// SessionReference is a non-bearer identifier for typed owner coordination.
// Only TenantProductService accepts the original browser credential; owner
// persistence and authorization contexts carry this reference, never the cookie.
func SessionReference(cookie string) string {
	h := sha256.Sum256([]byte(cookie))
	return "session_" + hex.EncodeToString(h[:])
}

// SessionCookieHeader is private gRPC response metadata between CloudIdentity
// and the BFF. The BFF delivers it only as an HttpOnly cookie, never in JSON.
const SessionCookieHeader = "x-opl-session-cookie"

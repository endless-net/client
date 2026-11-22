package enrollment

// Official product endpoints are release-owned. This only selects an expected
// browser URL; it never establishes trust or bypasses signed bootstrap validation.
const (
	ControlOrigin = "https://api.endlessnet.ru"
	BrowserOrigin = "https://admin.endlessnet.ru"
)

// NativeConsentOrigin keeps the existing same-origin rule for configured hosts
// and explicitly binds the official API to its official browser login origin.
func NativeConsentOrigin(origin string) string {
	if origin == ControlOrigin {
		return BrowserOrigin
	}
	return origin
}

// NativeConsentURL binds the Admin page to the original Identity transaction.
func NativeConsentURL(controlOrigin, transactionID string) string {
	return NativeConsentOrigin(controlOrigin) + "/native-login/" + transactionID
}

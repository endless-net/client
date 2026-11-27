package wintundriver

import _ "embed"

//go:embed wireguard-nt.json
var kernelManifestBytes []byte

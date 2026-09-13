package main

import "errors"

// Preserve classification through wrapped errors without interpreting diagnostics.
var errServerMapSigningTrustChanged = errors.New(serverMapSigningTrustChangedError)

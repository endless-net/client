// Package contractshard assigns complete control-plane scenarios to isolated CI runners.
package contractshard

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const Count = 2

var testName = regexp.MustCompile(`^TestControlPlane[A-Za-z0-9_]+$`)

var RequiredFlowRoots = []string{
	"TestControlPlaneNativeFlowConsentIPv4TCP",
	"TestControlPlaneNativeFlowConsentIPv4UDP",
	"TestControlPlaneNativeFlowConsentIPv6TCP",
	"TestControlPlaneNativeFlowConsentIPv6UDP",
}

// Durations from Windows 2022 repeat 1 of Actions run 36468528401, rounded to
// seconds. The longest roots determine balance; new roots receive defaultSeconds
// and are included automatically in the compiled inventory.
var historicalSeconds = map[string]int{
	"TestControlPlaneBrowserEnrollment":                      3,
	"TestControlPlaneBrowserEnrollmentExpiresDuringPolling":  7,
	"TestControlPlaneBrowserEnrollmentExpiryRecovery":        3,
	"TestControlPlaneBrowserEnrollmentInterrupted":           6,
	"TestControlPlaneBrowserEnrollmentRejectedDuringPolling": 7,
	"TestControlPlaneCLIIPCFailureBoundary":                  11,
	"TestControlPlaneCLIIPCUnaryTimeout":                     3,
	"TestControlPlaneDiagnosticsExport":                      8,
	"TestControlPlaneDNSProjection":                          4,
	"TestControlPlaneDNSUpstreamResponseBinding":             14,
	"TestControlPlaneDNSWireRecovery":                        34,
	"TestControlPlaneEphemeralLifecycle":                     15,
	"TestControlPlaneExitProvider":                           1,
	"TestControlPlaneInterruptedDisconnect":                  6,
	"TestControlPlaneIPCEvents":                              18,
	"TestControlPlaneIPCNegotiation":                         9,
	"TestControlPlaneIPCRequestValidation":                   7,
	"TestControlPlaneJoinTokenExpiryRecovery":                38,
	"TestControlPlaneJoinTokenRotation":                      37,
	"TestControlPlaneLifecycle":                              5,
	"TestControlPlaneLocalForgetAfterUnconfirmedLogout":      6,
	"TestControlPlaneLogoutRetryAfterControlRecovery":        7,
	"TestControlPlaneMalformedErrorsPreserveEnrollment":      27,
	"TestControlPlaneMapSigningRotation":                     43,
	"TestControlPlaneNativeApplicationRoute":                 115,
	"TestControlPlaneNativeCachedMapExpiry":                  103,
	"TestControlPlaneNativeDNSMapUpdates":                    13,
	"TestControlPlaneNativeExitRoute":                        29,
	"TestControlPlaneNativeFlowConsentIPv4TCP":               141,
	"TestControlPlaneNativeFlowConsentIPv4UDP":               140,
	"TestControlPlaneNativeFlowConsentIPv6TCP":               140,
	"TestControlPlaneNativeFlowConsentIPv6UDP":               140,
	"TestControlPlaneNativeIPv6TCPTraffic":                   68,
	"TestControlPlaneNativeIPv6UDPTraffic":                   57,
	"TestControlPlaneNativeLogoutTraffic":                    133,
	"TestControlPlaneNativeMachineSharing":                   116,
	"TestControlPlaneNativeMTUPreference":                    68,
	"TestControlPlaneNativeRelayFailover":                    37,
	"TestControlPlaneNativeRelayTraffic":                     35,
	"TestControlPlaneNativeServiceCatalog":                   47,
	"TestControlPlaneNativeSystemDNS":                        10,
	"TestControlPlaneNativeTCPTraffic":                       81,
	"TestControlPlaneNativeUDPTraffic":                       65,
	"TestControlPlaneNetworkSelectionBoundary":               7,
	"TestControlPlanePeerDeltaRecovery":                      5,
	"TestControlPlaneRecoveryErrorMatrix":                    47,
	"TestControlPlaneRegistrationResponseLoss":               1,
	"TestControlPlaneRejectsInvalidMaps":                     7,
	"TestControlPlaneRejectsRegistrationResponseMismatch":    3,
	"TestControlPlaneRouteAdvertisement":                     11,
	"TestControlPlaneRoutedResource":                         52,
	"TestControlPlaneSessionExpiryRecovery":                  51,
	"TestControlPlaneSingleAgentOwnership":                   15,
	"TestControlPlaneSubnetRouter":                           1,
	"TestControlPlaneTemporaryFailurePreservesEnrollment":    5,
	"TestControlPlaneTLSTrustBoundary":                       11,
	"TestControlPlaneTrustConfirmation":                      8,
}

const defaultSeconds = 20

func ParseInventory(data []byte) ([]string, error) {
	names := strings.Fields(string(data))
	if len(names) == 0 {
		return nil, errors.New("empty compiled test inventory")
	}
	slices.Sort(names)
	for i, name := range names {
		if !testName.MatchString(name) || i > 0 && names[i-1] == name {
			return nil, errors.New("invalid or duplicate compiled test name")
		}
	}
	return names, nil
}

func RequireFlowRoots(names []string) error {
	for _, root := range RequiredFlowRoots {
		if !slices.Contains(names, root) {
			return fmt.Errorf("missing required flow scenario %s", root)
		}
	}
	return nil
}

// Split uses longest-processing-time-first scheduling with name order as a
// deterministic tie-breaker. Each root is run once in its own runner's process.
func Split(names []string) [Count][]string {
	ordered := slices.Clone(names)
	slices.SortFunc(ordered, func(a, b string) int {
		wa, wb := weight(a), weight(b)
		if wa != wb {
			return wb - wa
		}
		return strings.Compare(a, b)
	})
	var groups [Count][]string
	var totals [Count]int
	for _, name := range ordered {
		index := 0
		if totals[1] < totals[0] {
			index = 1
		}
		groups[index] = append(groups[index], name)
		totals[index] += weight(name)
	}
	for i := range groups {
		slices.Sort(groups[i])
	}
	return groups
}

func Pattern(names []string) string {
	return "^(" + strings.Join(names, "|") + ")$"
}

func weight(name string) int {
	if seconds := historicalSeconds[name]; seconds > 0 {
		return seconds
	}
	return defaultSeconds
}

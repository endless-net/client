package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"strings"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func nativeRequestUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	raw := strings.ReplaceAll(value, "-", "")
	_, err := hex.DecodeString(raw)
	return err == nil && len(raw) == 32 && strings.Trim(raw, "0") != ""
}

// Explicit request identity and CAS values let scripts retain the exact command
// before sending. Never refresh/replay a mutation automatically on an error.
func cmdServiceRPCMutation(command string, args []string, output io.Writer) error {
	fs := flag.NewFlagSet("service "+command, flag.ContinueOnError)
	pipe, socket := serviceIPCTransportFlags(fs)
	timeoutValue := fs.String("timeout", "30s", "maximum time for native mutation acceptance")
	requestID := fs.String("request-id", "", "required durable mutation UUID; retain for operation lookup")
	instance := fs.String("expected-instance-id", "", "required runtime instance from a fresh snapshot")
	revision := fs.Uint64("expected-revision", 0, "required state revision from a fresh snapshot")
	var networkID string
	var exitID, exitFamily, exitLAN string
	if command == "select-exit-node" {
		fs.StringVar(&exitID, "exit-node-id", "", "required exact authorized exit node ID")
		fs.StringVar(&exitFamily, "family-mode", "", "required: ipv4-only, ipv6-only or dual-stack; runtime checks catalog permission")
		fs.StringVar(&exitLAN, "lan-access", "", "required: allow or block; runtime checks policy")
	}
	var patchJSON, resetKeys string
	if command == "set-preferences" {
		fs.StringVar(&patchJSON, "patch", "", "required protobuf JSON patch; omitted fields remain unchanged")
	}
	if command == "reset-preferences" {
		fs.StringVar(&resetKeys, "keys", "", "required comma-separated preference names whose user overrides are removed")
	}
	if command == "select-network" {
		fs.StringVar(&networkID, "network-id", "", "required exact network ID; names are not resolved")
	}
	var profile, displayName, controlOrigin string
	var enrollmentMode, hostname, tokenFile string
	var browser bool
	var confirmedOrigin, confirmedKey, confirmedAnnouncement string
	if command == "trust-server" {
		fs.StringVar(&confirmedOrigin, "confirmed-control-origin", "", "required exact control origin inspected by the operator")
		fs.StringVar(&confirmedKey, "confirmed-key-id", "", "required announced signing key ID inspected by the operator")
		fs.StringVar(&confirmedAnnouncement, "confirmed-announcement-id", "", "required announcement ID returned by server-identity")
	}
	if command == "enroll" {
		fs.StringVar(&enrollmentMode, "mode", "", "required: workstation, server, subnet-router or interactive")
		fs.StringVar(&hostname, "hostname", "", "hostname; empty lets runtime select the OS hostname")
		fs.StringVar(&tokenFile, "enrollment-token-file", "", "read enrollment token from file, or '-' for stdin")
		fs.BoolVar(&browser, "browser-login", false, "request browser approval")
	}
	var confirmed bool
	if command == "local-forget" {
		fs.BoolVar(&confirmed, "confirm-local-forget", false, "confirm local cleanup without confirmed remote revocation")
	}
	if command != "create-profile" {
		fs.StringVar(&profile, "profile-id", "", "required target profile UUID")
	}
	if command == "create-profile" || command == "rename-profile" {
		fs.StringVar(&displayName, "display-name", "", "required profile display name")
	}
	if command == "create-profile" {
		fs.StringVar(&controlOrigin, "control-origin", "", "required HTTPS control origin")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	family := map[string]ipc.ExitFamilyMode{"ipv4-only": ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, "ipv6-only": ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, "dual-stack": ipc.ExitFamilyMode_EXIT_FAMILY_MODE_DUAL_STACK}[exitFamily]
	lan := map[string]ipc.LanAccess{"allow": ipc.LanAccess_LAN_ACCESS_ALLOW, "block": ipc.LanAccess_LAN_ACCESS_BLOCK}[exitLAN]
	if command == "select-exit-node" && (strings.TrimSpace(exitID) == "" || family == ipc.ExitFamilyMode_EXIT_FAMILY_MODE_UNSPECIFIED || lan == ipc.LanAccess_LAN_ACCESS_UNSPECIFIED) {
		return fmt.Errorf("select-exit-node requires --exit-node-id, --family-mode (ipv4-only, ipv6-only or dual-stack) and --lan-access (allow or block)")
	}
	if command == "select-network" && strings.TrimSpace(networkID) == "" {
		return fmt.Errorf("--network-id is required")
	}
	if command == "local-forget" && !confirmed {
		return fmt.Errorf("local-forget requires --confirm-local-forget")
	}
	if command == "trust-server" {
		announcement, err := hex.DecodeString(confirmedAnnouncement)
		if strings.TrimSpace(confirmedOrigin) == "" || strings.TrimSpace(confirmedKey) == "" || err != nil || len(announcement) != 32 {
			return fmt.Errorf("trust-server requires --confirmed-control-origin, --confirmed-key-id and --confirmed-announcement-id (SHA-256 hex) from server-identity")
		}
	}
	if fs.NArg() != 0 || !nativeRequestUUID(*requestID) || strings.TrimSpace(*instance) == "" || *revision == 0 || (command != "create-profile" && strings.TrimSpace(profile) == "") {
		return fmt.Errorf("service %s requires --request-id UUID, --expected-instance-id and --expected-revision; non-create commands also require --profile-id; no positional arguments are allowed", command)
	}
	if (command == "create-profile" || command == "rename-profile") && strings.TrimSpace(displayName) == "" {
		return fmt.Errorf("--display-name is required")
	}
	if command == "create-profile" && strings.TrimSpace(controlOrigin) == "" {
		return fmt.Errorf("--control-origin is required")
	}
	if command != "set-preferences" && command != "reset-preferences" && command != "renew-session" && command != "connect" && command != "disconnect" && command != "logout" && command != "create-profile" && command != "select-profile" && command != "rename-profile" && command != "remove-profile" && command != "local-forget" && command != "enroll" && command != "trust-server" && command != "select-network" && command != "diagnostics-bundle" && command != "select-exit-node" && command != "clear-exit-node" {
		return fmt.Errorf("unknown native mutation %q", command)
	}
	timeout, err := parsePositiveServiceIPCTimeout(*timeoutValue)
	if err != nil {
		return err
	}
	var patch *ipc.PreferencesPatch
	var keys []ipc.PreferenceKey
	switch command {
	case "set-preferences":
		patch, err = nativePreferencePatch(patchJSON)
	case "reset-preferences":
		keys, err = nativePreferenceResetKeys(resetKeys)
	}
	if err != nil {
		return err
	}
	endpoint, err := nativeServiceEndpoint(*pipe, *socket)
	if err != nil {
		return err
	}
	var enrollment *ipc.EnrollRequest
	if command == "enroll" {
		if browser && tokenFile != "" {
			return fmt.Errorf("--browser-login and --enrollment-token-file are mutually exclusive")
		}
		token, err := secretFlagValue("enrollment-token", "", tokenFile)
		if err != nil {
			return err
		}
		enrollment, err = nativeCLIEnrollment(enrollmentMode, hostname, token, browser)
		if err != nil {
			return err
		}
	}
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		return err
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if _, err := consumer.Bootstrap(ctx); err != nil {
		return err
	}
	mutation := &ipc.MutationContext{RequestId: *requestID, ExpectedInstanceId: *instance, ExpectedRevision: *revision}
	ref := &ipc.ProfileRef{ProfileId: profile}
	var message proto.Message
	switch command {
	case "set-preferences":
		response, callErr := consumer.SetPreferences(ctx, connect.NewRequest(&ipc.SetPreferencesRequest{Mutation: mutation, Profile: ref, Patch: patch}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "reset-preferences":
		response, callErr := consumer.ResetPreferences(ctx, connect.NewRequest(&ipc.ResetPreferencesRequest{Mutation: mutation, Profile: ref, Keys: keys}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "renew-session":
		response, callErr := consumer.RenewSession(ctx, connect.NewRequest(&ipc.RenewSessionRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "select-network":
		response, callErr := consumer.SelectNetwork(ctx, connect.NewRequest(&ipc.SelectNetworkRequest{Mutation: mutation, Profile: ref, NetworkId: networkID}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "select-exit-node":
		response, callErr := consumer.SelectExitNode(ctx, connect.NewRequest(&ipc.SelectExitNodeRequest{Mutation: mutation, Profile: ref, ExitNodeId: exitID, FamilyMode: family, LanAccess: lan}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "clear-exit-node":
		response, callErr := consumer.ClearExitNode(ctx, connect.NewRequest(&ipc.ClearExitNodeRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "diagnostics-bundle":
		response, callErr := consumer.CreateDiagnosticsBundle(ctx, connect.NewRequest(&ipc.CreateDiagnosticsBundleRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "trust-server":
		response, callErr := consumer.TrustServerIdentity(ctx, connect.NewRequest(&ipc.TrustServerIdentityRequest{Mutation: mutation, Profile: ref, ConfirmedControlOrigin: confirmedOrigin, ConfirmedKeyId: confirmedKey, ConfirmedAnnouncementId: confirmedAnnouncement}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "enroll":
		enrollment.Mutation, enrollment.Profile = mutation, ref
		response, callErr := consumer.Enroll(ctx, connect.NewRequest(enrollment))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "local-forget":
		response, callErr := consumer.ForgetLocalEnrollment(ctx, connect.NewRequest(&ipc.ForgetLocalEnrollmentRequest{Mutation: mutation, Profile: ref, Confirmed: confirmed}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "create-profile":
		response, callErr := consumer.CreateProfile(ctx, connect.NewRequest(&ipc.CreateProfileRequest{Mutation: mutation, DisplayName: displayName, ControlOrigin: controlOrigin}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "select-profile":
		response, callErr := consumer.SelectProfile(ctx, connect.NewRequest(&ipc.SelectProfileRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "rename-profile":
		response, callErr := consumer.RenameProfile(ctx, connect.NewRequest(&ipc.RenameProfileRequest{Mutation: mutation, Profile: ref, DisplayName: displayName}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "remove-profile":
		response, callErr := consumer.RemoveProfile(ctx, connect.NewRequest(&ipc.RemoveProfileRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "connect":
		response, callErr := consumer.Connect(ctx, connect.NewRequest(&ipc.ConnectRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "disconnect":
		response, callErr := consumer.Disconnect(ctx, connect.NewRequest(&ipc.DisconnectRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	case "logout":
		response, callErr := consumer.Logout(ctx, connect.NewRequest(&ipc.LogoutRequest{Mutation: mutation, Profile: ref}))
		err = callErr
		if err == nil {
			message = response.Msg
		}
	}
	if err == nil {
		var encoded []byte
		encoded, err = protojson.Marshal(message)
		if err == nil {
			_, err = fmt.Fprintln(output, string(encoded))
		}
	}
	if err != nil {
		return fmt.Errorf("service %s: %w; inspect service operation --request-id %s using the same endpoint before deciding whether to retry", command, err, *requestID)
	}
	return nil
}

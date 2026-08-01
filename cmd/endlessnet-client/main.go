package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/endless-net/client/internal/client"
	ipc "github.com/endless-net/client/ipc/v2"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	wgkeys "github.com/endless-net/client-api/clientapi/wireguard"

	relayauth "github.com/endless-net/relay/protocol/v1"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

var newServiceIPCClient = func(endpoint string) *ipc.Client {
	local, err := ipc.NewLocalClient(endpoint)
	if err != nil {
		return nil
	}
	return local
}

var loginDiscoveryHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

const (
	defaultNetworkName                = "default"
	defaultPublicServerURL            = "https://api.endlessnet.ru"
	nodeEnrollmentPollDefaultInterval = 5 * time.Second
	managedEnrollmentPollInterval     = 2 * time.Second
	managedServiceIPCStartupTimeout   = 10 * time.Second
)

func clientConfigErrorCode(err error) string {
	if client.IsClientStateFormatUnsupported(err) {
		return client.ClientStateFormatUnsupportedCode
	}
	if client.IsClientStateVersionUnsupported(err) {
		return client.ClientStateVersionUnsupportedCode
	}
	return cliErrorConfigUnavailable
}

func serviceIPCConfigError(err error) ipc.Error {
	status := http.StatusInternalServerError
	if client.IsClientStateFormatUnsupported(err) || client.IsClientStateVersionUnsupported(err) {
		status = http.StatusConflict
	}
	return ipc.NewError(status, clientConfigErrorCode(err), err)
}

const (
	cliErrorInvalidTimeout         = "invalid_timeout"
	cliErrorInvalidRelayTimeout    = "invalid_relay_timeout"
	cliErrorInvalidArguments       = "invalid_arguments"
	cliErrorWGInterfaceRequired    = "wg_interface_required"
	cliErrorConfigUnavailable      = "config_unavailable"
	cliErrorAgentStateUnavailable  = "agent_state_unavailable"
	cliErrorControlMetricsFailed   = "control_metrics_failed"
	cliErrorNetworkMapUnavailable  = "network_map_unavailable"
	cliErrorSTUNEndpointsMissing   = "stun_endpoints_missing"
	cliErrorSTUNUnreachable        = "stun_unreachable"
	cliErrorRelayTLSConfigInvalid  = "relay_tls_config_invalid"
	cliErrorRelayEndpointsMissing  = "relay_endpoints_missing"
	cliErrorRelayCredentialMissing = "relay_credential_missing"
	cliErrorRelayUnavailable       = "relay_unavailable"
	cliErrorPathProbeFailed        = "path_probe_failed"
	cliErrorPathUnavailable        = "path_unavailable"
	cliErrorPeerTargetInvalid      = "peer_target_invalid"
	cliErrorPeerUnreachable        = "peer_unreachable"
	cliErrorRouteConflict          = "route_conflict"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "login":
		err = cmdLogin(os.Args[2:])
	case "keygen":
		err = cmdKeygen()
	case "version":
		err = cmdVersion()
	case "network":
		err = cmdNetwork(os.Args[2:])
	case "join-token":
		err = cmdJoinToken(os.Args[2:])
	case "billing":
		err = cmdBilling(os.Args[2:])
	case "nodes":
		err = cmdNodes(os.Args[2:])
	case "up":
		if isFriendlyCLIExecutable(os.Args[0]) {
			err = cmdManagedUp(os.Args[2:])
		} else {
			err = cmdUp(os.Args[2:])
		}
	case "sync":
		err = cmdSync(os.Args[2:])
	case "export":
		err = cmdExport(os.Args[2:])
	case "agent":
		err = cmdAgent(os.Args[2:])
	case "service":
		err = cmdService(os.Args[2:])
	case "state":
		err = cmdState(os.Args[2:])
	case "down":
		err = cmdDown(os.Args[2:])
	case "diagnostics":
		err = cmdDiagnostics(os.Args[2:])
	case "relay-check":
		err = cmdRelayCheck(os.Args[2:])
	case "relay-bridge":
		err = cmdRelayBridge(os.Args[2:])
	case "path-check":
		err = cmdPathCheck(os.Args[2:])
	case "ping":
		err = cmdPing(os.Args[2:])
	case "netcheck":
		err = cmdNetcheck(os.Args[2:])
	case "dns":
		err = cmdDNS(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "logout":
		err = cmdLogout(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func isFriendlyCLIExecutable(path string) bool {
	path = strings.ReplaceAll(path, `\`, "/")
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.EqualFold(name, "endlessnet")
}

func writeJSONErrorPayload(code string, err error) error {
	if err == nil {
		err = fmt.Errorf("command failed")
	}
	payload := map[string]any{
		"ok":         false,
		"error_code": code,
		"error":      err.Error(),
	}
	raw, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	fmt.Println(string(raw))
	return err
}

func writeDiagnosticsErrorPayload(outputPath, code string, err error) error {
	if err == nil {
		err = fmt.Errorf("diagnostics failed")
	}
	payload := map[string]any{
		"ok":         false,
		"error_code": code,
		"error":      err.Error(),
	}
	raw, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	raw = append(raw, '\n')
	if strings.TrimSpace(outputPath) == "" {
		fmt.Print(string(raw))
		return err
	}
	if writeErr := os.WriteFile(outputPath, raw, 0o600); writeErr != nil {
		return fmt.Errorf("%v; additionally failed to write diagnostics error payload: %w", err, writeErr)
	}
	return err
}

func relayErrorCode(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "does not contain relay endpoints"):
		return cliErrorRelayEndpointsMissing
	case strings.Contains(text, "relay credential is missing"):
		return cliErrorRelayCredentialMissing
	default:
		return cliErrorRelayUnavailable
	}
}

func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	serverURL := fs.String("server", "http://localhost:8080", "EndlessNet server URL")
	token := fs.String("token", "", "session token for loopback development only; use --token-file elsewhere")
	tokenFile := fs.String("token-file", "", "read a pre-provisioned session token from this file, or '-' for stdin")
	configPath := fs.String("config", "", "client config path")
	mapSigningTrustFile := fs.String("map-signing-trust-file", "", "trusted map-signing bundle JSON file")
	coordinators := multiFlag{}
	fs.Var(&coordinators, "coordinator", "additional coordinator URL; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sessionToken, err := secretFlagValue("token", *token, *tokenFile)
	if err != nil {
		return err
	}
	server := strings.TrimRight(*serverURL, "/")
	if sessionToken == "" {
		managementURL, err := discoverManagementURL(context.Background(), server)
		if err != nil {
			return err
		}
		fmt.Printf("Open %s and sign in with your browser.\n", managementURL)
		fmt.Println("Browser authentication stays in an HttpOnly cookie and does not return a CLI session token.")
		fmt.Println("Create a one-time node join token through the administrative API or MCP, save it in an owner-only file, then run:")
		fmt.Printf("endlessnet-client up --server %s --join-token-file <join-token-file>\n", server)
		return nil
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	primaryURL := server
	if flagWasSet(fs, "coordinator") {
		cfg.ControlPlaneURLs = clientapi.NormalizeControlPlaneURLs(append([]string{primaryURL}, coordinators...)...)
	} else {
		cfg.ControlPlaneURLs = clientapi.NormalizeControlPlaneURLs(append([]string{primaryURL}, cfg.ControlPlaneURLs...)...)
	}
	if strings.TrimSpace(*token) != "" {
		if err := requireLoopbackInlineSessionToken(cfg.ControlURLs()); err != nil {
			return err
		}
	}
	managementURL, err := discoverManagementURLFromControlPlanes(context.Background(), cfg.ControlURLs())
	if err != nil {
		return err
	}
	cfg.ManagementURL = managementURL
	cfg.Token = sessionToken
	if err := applyMapSigningTrustOption(&cfg, *mapSigningTrustFile); err != nil {
		return err
	}
	if err := enrollMapSigningTrust(&cfg, clientapi.NewAPIWithBaseURLs(cfg.ControlURLs(), cfg.Token)); err != nil {
		return err
	}
	if err := client.SaveConfig(*configPath, cfg); err != nil {
		return err
	}
	fmt.Println("login saved")
	return nil
}

func discoverManagementURL(ctx context.Context, controlPlaneURL string) (string, error) {
	controlPlaneURL = strings.TrimRight(strings.TrimSpace(controlPlaneURL), "/")
	if controlPlaneURL == "" {
		return "", errors.New("control-plane URL is required")
	}
	parsedControlPlane, err := url.Parse(controlPlaneURL)
	if err != nil || !isSecureOriginURL(parsedControlPlane) {
		return "", errors.New("control-plane URL must be an HTTPS origin, except for loopback development")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, controlPlaneURL+"/.well-known/endlessnet", nil)
	if err != nil {
		return "", err
	}
	response, err := loginDiscoveryHTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("discover management URL: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("discover management URL: control plane returned %s", response.Status)
	}
	var document struct {
		ManagementURL string `json:"management_url"`
	}
	const maxDiscoveryDocumentBytes = 64 * 1024
	rawDocument, err := io.ReadAll(io.LimitReader(response.Body, maxDiscoveryDocumentBytes+1))
	if err != nil {
		return "", fmt.Errorf("read management discovery: %w", err)
	}
	if len(rawDocument) > maxDiscoveryDocumentBytes {
		return "", errors.New("decode management discovery: document is too large")
	}
	decoder := json.NewDecoder(strings.NewReader(string(rawDocument)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return "", fmt.Errorf("decode management discovery: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", errors.New("decode management discovery: multiple JSON values")
	}
	managementURL := strings.TrimSpace(document.ManagementURL)
	parsedManagement, err := url.Parse(managementURL)
	if err != nil || !isSecureOriginURL(parsedManagement) {
		return "", errors.New("management discovery returned an invalid URL")
	}
	parsedManagement.Path = "/"
	return parsedManagement.String(), nil
}

func discoverManagementURLFromControlPlanes(ctx context.Context, controlPlaneURLs []string) (string, error) {
	controlPlaneURLs = clientapi.NormalizeControlPlaneURLs(controlPlaneURLs...)
	if len(controlPlaneURLs) == 0 {
		return "", errors.New("at least one control-plane URL is required")
	}
	var attemptErrors []error
	for index, controlPlaneURL := range controlPlaneURLs {
		managementURL, err := discoverManagementURL(ctx, controlPlaneURL)
		if err == nil {
			return managementURL, nil
		}
		attemptErrors = append(attemptErrors, fmt.Errorf("control-plane attempt %d: %w", index+1, err))
	}
	return "", fmt.Errorf("discover management URL from control planes: %w", errors.Join(attemptErrors...))
}

func isSecureOriginURL(parsed *url.URL) bool {
	if parsed == nil || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	return parsed.Scheme == "https" || isLoopbackHTTPURL(parsed)
}

func isLoopbackHTTPURL(parsed *url.URL) bool {
	if parsed == nil || parsed.Scheme != "http" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func cmdKeygen() error {
	privateKey, err := wgkeys.GeneratePrivateKey()
	if err != nil {
		return err
	}
	publicKey, err := wgkeys.PublicKey(privateKey)
	if err != nil {
		return err
	}
	fmt.Printf("PrivateKey = %s\n", privateKey)
	fmt.Printf("PublicKey = %s\n", publicKey)
	return nil
}

func cmdVersion() error {
	fmt.Printf("endlessnet-client %s\n", version)
	fmt.Printf("commit: %s\n", commit)
	fmt.Printf("built: %s\n", buildDate)
	fmt.Printf("target: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

func cmdNetwork(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("network command requires create or list")
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("network create", flag.ExitOnError)
		name := fs.String("name", "", "network name")
		cidr := fs.String("cidr", "100.64.0.0/24", "network CIDR")
		ipv6CIDR := fs.String("ipv6-cidr", "", "optional IPv6 overlay CIDR")
		accountID := fs.String("account", "", "billing account/workspace ID")
		idempotencyKey := fs.String("idempotency-key", "", "confidential retry key (base64url encoding 32 random bytes)")
		dns := multiFlag{}
		configPath := fs.String("config", "", "client config path")
		fs.Var(&dns, "dns", "DNS server IP; repeatable")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		_ = cfg
		network, err := api.CreateNetwork(clientapi.CreateNetworkRequest{Name: *name, CIDR: *cidr, IPv6CIDR: *ipv6CIDR, DNS: dns, AccountID: *accountID, IdempotencyKey: *idempotencyKey})
		if err != nil {
			return err
		}
		fmt.Printf("%s\t%s\t%s\n", network.ID, network.Name, network.CIDR)
	case "list":
		fs := flag.NewFlagSet("network list", flag.ExitOnError)
		configPath := fs.String("config", "", "client config path")
		accountID := fs.String("account", "", "billing account/workspace ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		networks, err := api.ListNetworksForAccount(*accountID)
		if err != nil {
			return err
		}
		for _, network := range networks {
			fmt.Printf("%s\t%s\t%s\n", network.ID, network.Name, network.CIDR)
		}
	case "routes":
		fs := flag.NewFlagSet("network routes", flag.ExitOnError)
		network := fs.String("network", defaultNetworkName, "network name or ID")
		configPath := fs.String("config", "", "client config path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		routes, err := api.ListAdvertisedRoutes(resolveNetworkFlag(*network))
		if err != nil {
			return err
		}
		for _, route := range routes {
			status := "pending"
			if route.Approved {
				status = "approved"
			}
			fmt.Printf("%s\t%s\t%s\t%s\n", route.NodeID, route.Hostname, route.CIDR, status)
		}
	case "approve-route", "revoke-route":
		approved := args[0] == "approve-route"
		fs := flag.NewFlagSet("network "+args[0], flag.ExitOnError)
		network := fs.String("network", defaultNetworkName, "network name or ID")
		nodeID := fs.String("node", "", "node ID advertising the route")
		cidr := fs.String("cidr", "", "advertised route CIDR")
		configPath := fs.String("config", "", "client config path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*nodeID) == "" {
			return fmt.Errorf("--node is required")
		}
		if strings.TrimSpace(*cidr) == "" {
			return fmt.Errorf("--cidr is required")
		}
		_, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		response, err := api.SetAdvertisedRouteApproval(resolveNetworkFlag(*network), clientapi.SetAdvertisedRouteApprovalRequest{
			NodeID:   *nodeID,
			CIDR:     *cidr,
			Approved: approved,
		})
		if err != nil {
			return err
		}
		status := "pending"
		if response.Route.Approved {
			status = "approved"
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%d\n", response.Route.NodeID, response.Route.Hostname, response.Route.CIDR, status, response.Network.Revision)
	default:
		return fmt.Errorf("unknown network command %q", args[0])
	}
	return nil
}

func cmdJoinToken(args []string) error {
	if len(args) < 1 || args[0] != "create" {
		return fmt.Errorf("join-token command requires create")
	}
	fs := flag.NewFlagSet("join-token create", flag.ExitOnError)
	network := fs.String("network", defaultNetworkName, "network name or ID")
	ttl := fs.String("ttl", "1h", "join token TTL")
	idempotencyKey := fs.String("idempotency-key", "", "confidential retry key (base64url encoding 32 random bytes)")
	reusable := fs.Bool("reusable", false, "allow the join token to enroll more than one node")
	ephemeral := fs.Bool("ephemeral", false, "mark nodes enrolled with this token as ephemeral")
	preauthorized := fs.Bool("preauthorized", false, "approve nodes enrolled with this token without a separate admin action")
	tags := multiFlag{}
	configPath := fs.String("config", "", "client config path")
	fs.Var(&tags, "tag", "node tag granted by this token; repeatable")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	_, api, err := loadAPI(*configPath)
	if err != nil {
		return err
	}
	req := clientapi.CreateJoinTokenRequest{
		TTL: *ttl, IdempotencyKey: *idempotencyKey, Reusable: *reusable,
		Ephemeral: *ephemeral, Preauthorized: *preauthorized, Tags: append([]string(nil), tags...),
	}
	setJoinTokenNetworkRef(&req, resolveNetworkFlag(*network))
	token, err := api.CreateJoinToken(req)
	if err != nil {
		return err
	}
	fmt.Printf("%s\t%s\t%s\n", token.ID, token.Token, token.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"))
	return nil
}

func cmdBilling(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("billing command requires plans, accounts, summary, checkout, or invoices")
	}
	switch args[0] {
	case "plans":
		fs := flag.NewFlagSet("billing plans", flag.ExitOnError)
		configPath := fs.String("config", "", "client config path")
		jsonOutput := fs.Bool("json", false, "write JSON output")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		_, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		plans, err := api.BillingPlans()
		if err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(plans)
		}
		for _, plan := range plans {
			if !plan.Public {
				continue
			}
			price := "custom"
			if plan.MonthlyPrice >= 0 {
				price = amountLabel(plan.MonthlyPrice, plan.Currency) + "/month"
			}
			fmt.Printf("%s\t%s\t%s\t%s\n", plan.ID, plan.Name, price, plan.BillingProvider)
		}
	case "accounts":
		fs := flag.NewFlagSet("billing accounts", flag.ExitOnError)
		configPath := fs.String("config", "", "client config path")
		useAccount := fs.String("use", "", "save active billing account id")
		jsonOutput := fs.Bool("json", false, "write JSON output")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		accounts, err := api.ListAccounts()
		if err != nil {
			return err
		}
		if strings.TrimSpace(*useAccount) != "" {
			found := false
			for _, account := range accounts {
				if account.ID == strings.TrimSpace(*useAccount) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("account %q is not available", *useAccount)
			}
			cfg.ActiveAccountID = strings.TrimSpace(*useAccount)
			if err := client.SaveConfig(*configPath, cfg); err != nil {
				return err
			}
		}
		if *jsonOutput {
			return printJSON(map[string]any{"accounts": accounts, "active_account_id": cfg.ActiveAccountID})
		}
		for _, account := range accounts {
			active := ""
			if account.ID == cfg.ActiveAccountID {
				active = "*"
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", active, account.ID, account.Name, account.Type, account.Status)
		}
	case "summary":
		fs := flag.NewFlagSet("billing summary", flag.ExitOnError)
		configPath := fs.String("config", "", "client config path")
		accountID := fs.String("account", "", "account id")
		jsonOutput := fs.Bool("json", false, "write JSON output")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		resolved, err := resolveBillingAccount(cfg, api, *accountID)
		if err != nil {
			return err
		}
		subscription, err := api.AccountSubscription(resolved)
		if err != nil {
			return err
		}
		entitlements, err := api.AccountEntitlements(resolved)
		if err != nil {
			return err
		}
		usage, err := api.AccountUsage(resolved)
		if err != nil {
			return err
		}
		summary := map[string]any{"account_id": resolved, "subscription": subscription, "entitlements": entitlements, "usage": usage}
		if *jsonOutput {
			return printJSON(summary)
		}
		fmt.Printf("account\t%s\nplan\t%s\nstatus\t%s\nusers\t%d/%d\nnodes\t%d/%d\nnetworks\t%d/%d\n",
			resolved,
			entitlements.PlanID,
			entitlements.PlanStatus,
			usage.Users,
			entitlements.Limits["users"],
			usage.Nodes,
			entitlements.Limits["nodes"],
			usage.Networks,
			entitlements.Limits["networks"],
		)
	case "checkout":
		fs := flag.NewFlagSet("billing checkout", flag.ExitOnError)
		configPath := fs.String("config", "", "client config path")
		accountID := fs.String("account", "", "account id")
		planID := fs.String("plan", "", "plan id")
		period := fs.String("period", "monthly", "billing period")
		jsonOutput := fs.Bool("json", false, "write JSON output")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*planID) == "" {
			return fmt.Errorf("--plan is required")
		}
		cfg, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		resolved, err := resolveBillingAccount(cfg, api, *accountID)
		if err != nil {
			return err
		}
		checkout, err := api.CreateCheckout(resolved, clientapi.BillingCheckoutRequest{PlanID: *planID, BillingPeriod: *period})
		if err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(checkout)
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", checkout.ID, checkout.Status, checkout.Provider, checkout.ConfirmationURL)
	case "invoices":
		fs := flag.NewFlagSet("billing invoices", flag.ExitOnError)
		configPath := fs.String("config", "", "client config path")
		accountID := fs.String("account", "", "account id")
		jsonOutput := fs.Bool("json", false, "write JSON output")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, api, err := loadAPI(*configPath)
		if err != nil {
			return err
		}
		resolved, err := resolveBillingAccount(cfg, api, *accountID)
		if err != nil {
			return err
		}
		invoices, err := api.ListInvoices(resolved)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return printJSON(invoices)
		}
		for _, invoice := range invoices {
			fmt.Printf("%s\t%s\t%s\t%s\n", invoice.ID, invoice.Status, amountLabel(invoice.Amount, invoice.Currency), invoice.Number)
		}
	default:
		return fmt.Errorf("unknown billing command %q", args[0])
	}
	return nil
}

func cmdNodes(args []string) error {
	if len(args) < 1 || args[0] != "list" {
		return fmt.Errorf("nodes command requires list")
	}
	fs := flag.NewFlagSet("nodes list", flag.ExitOnError)
	network := fs.String("network", defaultNetworkName, "network name or ID")
	configPath := fs.String("config", "", "client config path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	_, api, err := loadAPI(*configPath)
	if err != nil {
		return err
	}
	nodes, err := api.ListNodes(resolveNetworkFlag(*network))
	if err != nil {
		return err
	}
	for _, node := range nodes {
		fmt.Printf("%s\t%s\t%s\t%s\n", node.ID, node.Hostname, node.AssignedIP, node.Endpoint)
	}
	return nil
}

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	network := fs.String("network", defaultNetworkName, "network name or ID")
	serverURL := fs.String("server", "", "EndlessNet server URL")
	coordinators := multiFlag{}
	joinToken := fs.String("join-token", "", "one-time node join token")
	joinTokenFile := fs.String("join-token-file", "", "read one-time node join token from this file, or '-' for stdin")
	idempotencyKey := fs.String("idempotency-key", "", "registration retry idempotency key")
	hostname := fs.String("hostname", mustHostname(), "node hostname")
	endpoint := fs.String("endpoint", "", "public WireGuard endpoint host:port")
	wireGuardMTU := fs.Int("mtu", 0, "WireGuard interface MTU; 0 uses the wireguard-go default (1420)")
	wireGuardRouteTable := fs.String("route-table", "", "WireGuard route table: auto, off, or numeric table ID")
	configPath := fs.String("config", "", "client config path")
	mapSigningTrustFile := fs.String("map-signing-trust-file", "", "trusted map-signing bundle JSON file")
	approvalTimeoutValue := fs.String("approval-timeout", "10m", "maximum time to wait for browser approval when no join token is provided; 0 disables waiting")
	advertiseSNAT := fs.Bool("advertise-snat", false, "enable forwarding and SNAT hooks for this node's advertised IPv4 routes")
	exitLANPolicy := fs.String("exit-lan-policy", "", "exit-node LAN policy for local networks: allow or block")
	advertise := multiFlag{}
	tags := multiFlag{}
	fs.Var(&coordinators, "coordinator", "additional coordinator URL; repeatable")
	fs.Var(&advertise, "advertise", "route/CIDR advertised by this node; repeatable")
	fs.Var(&tags, "tag", "node tag; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	approvalTimeout, err := parseOptionalDuration("approval-timeout", *approvalTimeoutValue)
	if err != nil {
		return err
	}
	effectiveJoinToken, err := secretFlagValue("join-token", *joinToken, *joinTokenFile)
	if err != nil {
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) != "" {
		cfg.ControlPlaneURLs = clientapi.NormalizeControlPlaneURLs(append([]string{strings.TrimRight(*serverURL, "/")}, cfg.ControlPlaneURLs...)...)
	}
	if flagWasSet(fs, "coordinator") {
		cfg.ControlPlaneURLs = clientapi.NormalizeControlPlaneURLs(append([]string{firstControlPlaneURL(cfg)}, coordinators...)...)
	}
	if err := applyMapSigningTrustOption(&cfg, *mapSigningTrustFile); err != nil {
		return err
	}
	if err := updateExitLANPolicyFromFlag(fs, *exitLANPolicy, &cfg); err != nil {
		return err
	}
	if err := updateWireGuardMTUFromFlag(fs, *wireGuardMTU, &cfg); err != nil {
		return err
	}
	if err := updateWireGuardRouteTableFromFlag(fs, *wireGuardRouteTable, &cfg); err != nil {
		return err
	}
	if *advertiseSNAT {
		cfg.SubnetRouterSNAT = true
	}
	if len(cfg.ControlURLs()) == 0 {
		return fmt.Errorf("server URL is required; run login first or pass --server")
	}
	browserEnrollment := strings.TrimSpace(effectiveJoinToken) == "" &&
		strings.TrimSpace(cfg.NodeCredential) == "" &&
		strings.TrimSpace(cfg.Token) == ""
	keysChanged := false
	if strings.TrimSpace(cfg.IdentityPrivateKey) == "" {
		cfg.IdentityPrivateKey, err = client.GenerateIdentityPrivateKey()
		if err != nil {
			return err
		}
		keysChanged = true
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		cfg.PrivateKey, err = wgkeys.GeneratePrivateKey()
		if err != nil {
			return err
		}
		keysChanged = true
	}
	if keysChanged {
		if err := client.SaveConfig(*configPath, cfg); err != nil {
			return err
		}
	}
	api := apiFromConfig(cfg)
	if !client.HasSigningTrust(cfg) {
		if strings.TrimSpace(effectiveJoinToken) == "" && strings.TrimSpace(cfg.Token) == "" && !browserEnrollment {
			return errors.New("map signing trust anchor is required")
		}
		if err := enrollMapSigningTrust(&cfg, api); err != nil {
			return err
		}
	} else if err := refreshMapSigningTrust(&cfg, api); err != nil {
		return err
	}
	publicKey, err := wgkeys.PublicKey(cfg.PrivateKey)
	if err != nil {
		return err
	}
	identityPublicKey, err := client.IdentityPublicKey(cfg.IdentityPrivateKey)
	if err != nil {
		return err
	}
	deviceFingerprint, err := client.DeviceFingerprint(firstControlPlaneURL(cfg), publicKey)
	if err != nil {
		return err
	}
	if _, err := client.BindConfigDeviceFingerprint(&cfg, deviceFingerprint); err != nil {
		return err
	}
	effectiveIdempotencyKey := strings.TrimSpace(*idempotencyKey)
	if browserEnrollment && effectiveIdempotencyKey == "" {
		effectiveIdempotencyKey, err = clientapi.NewCreateIdempotencyKey()
		if err != nil {
			return err
		}
	}
	req := clientapi.RegisterNodeRequest{
		IdempotencyKey:    effectiveIdempotencyKey,
		Hostname:          *hostname,
		ClientVersion:     strings.TrimSpace(version),
		IdentityPublicKey: identityPublicKey,
		PublicKey:         publicKey,
		DeviceFingerprint: deviceFingerprint,
		Endpoint:          *endpoint,
		AdvertisedIPs:     advertise,
		Tags:              tags,
	}
	if strings.TrimSpace(effectiveJoinToken) != "" {
		req.JoinToken = strings.TrimSpace(effectiveJoinToken)
	} else if strings.TrimSpace(cfg.NodeCredential) != "" && strings.TrimSpace(cfg.Token) == "" {
		req.NodeCredential = cfg.NodeCredential
	} else if strings.TrimSpace(cfg.Token) == "" {
		browserEnrollment = true
	} else {
		req.SessionTokenBinding = clientapi.RegistrationSessionTokenBinding(cfg.Token)
	}
	if strings.TrimSpace(req.NodeCredential) == "" {
		setNetworkRef(&req, resolveNetworkFlag(*network))
	}
	identitySignature, err := client.SignIdentity(cfg.IdentityPrivateKey, clientapi.RegistrationIdentityProofPayload(req))
	if err != nil {
		return err
	}
	req.IdentitySignature = identitySignature
	var response clientapi.RegisterNodeResponse
	if browserEnrollment {
		response, err = waitForBrowserEnrollmentApproval(api, &cfg, *configPath, &req, approvalTimeout)
	} else {
		response, err = api.RegisterNode(req)
	}
	if err != nil {
		return err
	}
	if err := validateRegistrationResponseBinding(cfg, req, response, publicKey, identityPublicKey, deviceFingerprint); err != nil {
		return err
	}
	if err := verifyNetworkMap(&cfg, response); err != nil {
		return fmt.Errorf("verify signed registration response: %w", err)
	}
	registrationCredential := strings.TrimSpace(response.NodeCredential)
	if registrationCredential == "" {
		registrationCredential = firstNonEmpty(req.NodeCredential, cfg.NodeCredential)
	}
	if err := verifyRegistrationNodeCredential(api, response, registrationCredential); err != nil {
		return err
	}
	approvalState := strings.ToLower(strings.TrimSpace(response.Node.ApprovalState))
	if approvalState == clientapi.NodeApprovalPending || approvalState == clientapi.NodeApprovalRejected {
		if strings.TrimSpace(response.NodeCredential) != "" {
			cfg.NodeCredential = response.NodeCredential
		}
		if strings.TrimSpace(cfg.NodeCredential) == "" {
			return errors.New("pending enrollment response is missing node credential")
		}
		cfg.NodeID = strings.TrimSpace(response.Node.ID)
		cfg.NetworkID = strings.TrimSpace(response.Network.ID)
		cfg.NodeApprovalState = approvalState
		cfg.MapRevision = 0
		cfg.CachedMap = nil
		cfg.CachedMapSavedAt = nil
		if strings.TrimSpace(effectiveJoinToken) != "" || strings.TrimSpace(cfg.Token) == "" {
			cfg.Token = ""
		}
		if _, err := client.BindConfigDeviceFingerprint(&cfg, deviceFingerprint); err != nil {
			return err
		}
		if err := client.SaveConfig(*configPath, cfg); err != nil {
			return err
		}
		if approvalState == clientapi.NodeApprovalRejected {
			return errors.New("node enrollment was rejected")
		}
		fmt.Printf("node %s is pending approval\n", response.Node.ID)
		return nil
	}
	if err := cacheNetworkMapChecked(&cfg, response); err != nil {
		return err
	}
	if strings.TrimSpace(response.NodeCredential) != "" {
		cfg.NodeCredential = response.NodeCredential
		if strings.TrimSpace(effectiveJoinToken) != "" || strings.TrimSpace(cfg.Token) == "" {
			cfg.Token = ""
		}
	}
	cfg.EnrollmentRequestID = ""
	cfg.EnrollmentPollToken = ""
	cfg.ApprovalURL = ""
	cfg.EnrollmentRequest = nil
	if _, err := client.BindConfigDeviceFingerprint(&cfg, deviceFingerprint); err != nil {
		return err
	}
	if err := client.SaveConfig(*configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("enrolled node %s (%s); wireguard-go runtime is managed by the agent\n", response.Node.Hostname, response.Node.AssignedIP)
	return nil
}

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	wireGuardMTU := fs.Int("mtu", 0, "WireGuard interface MTU; 0 uses the wireguard-go default (1420)")
	wireGuardRouteTable := fs.String("route-table", "", "WireGuard route table: auto, off, or numeric table ID")
	configPath := fs.String("config", "", "client config path")
	timeoutValue := fs.String("timeout", "10s", "maximum time to wait for a map stream event")
	fromRevision := fs.Uint64("from-revision", 0, "map revision checkpoint; defaults to the saved client map_revision")
	offline := fs.Bool("offline", false, "validate and retain the cached signed network map without contacting the server")
	endpoint := fs.String("endpoint", "", "published WireGuard endpoint host:port; updates the coordinator only when changed")
	maxCacheAgeValue := fs.String("max-cache-age", "0s", "maximum accepted cached map age in offline mode; 0 disables the age check")
	exitLANPolicy := fs.String("exit-lan-policy", "", "exit-node LAN policy for local networks: allow or block")
	if err := fs.Parse(args); err != nil {
		return err
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		return fmt.Errorf("timeout must be a positive duration")
	}
	maxCacheAge, err := parseOptionalDuration("max-cache-age", *maxCacheAgeValue)
	if err != nil {
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return fmt.Errorf("private key is missing; run up first")
	}
	if err := updateExitLANPolicyFromFlag(fs, *exitLANPolicy, &cfg); err != nil {
		return err
	}
	if err := updateWireGuardMTUFromFlag(fs, *wireGuardMTU, &cfg); err != nil {
		return err
	}
	if err := updateWireGuardRouteTableFromFlag(fs, *wireGuardRouteTable, &cfg); err != nil {
		return err
	}
	if *offline {
		response, err := verifiedCachedNetworkMapWithMaxAge(&cfg, maxCacheAge)
		if err != nil {
			return err
		}
		if err := client.SaveConfig(*configPath, cfg); err != nil {
			return err
		}
		fmt.Printf("restored cached map revision %d\n", response.Network.Revision)
		return nil
	}
	if len(cfg.ControlURLs()) == 0 {
		return fmt.Errorf("server URL is required; run up or login first")
	}
	if strings.TrimSpace(cfg.NodeID) == "" || strings.TrimSpace(cfg.NodeCredential) == "" {
		return fmt.Errorf("node identity is missing; run up first")
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		return err
	}
	streamFromRevision := cfg.MapRevision
	if flagWasSet(fs, "from-revision") {
		streamFromRevision = *fromRevision
	}
	api := apiFromConfig(cfg)
	api.HTTPClient.Timeout = timeout + 5*time.Second
	if err := refreshMapSigningTrust(&cfg, api); err != nil {
		return err
	}
	if strings.TrimSpace(*endpoint) != "" {
		networkMap, endpointChanged, err := updatePublishedEndpointFromConfig(&cfg, api, *endpoint)
		if err != nil {
			return err
		}
		if endpointChanged {
			if err := client.SaveConfig(*configPath, cfg); err != nil {
				return err
			}
			fmt.Printf("endpoint-updated map revision %d\n", networkMap.Network.Revision)
			return nil
		}
	}
	event, err := api.ReadMapStreamEvent(cfg.NodeID, mapStreamCursor(cfg, streamFromRevision), timeout)
	if err != nil {
		return err
	}
	networkMap, cacheAction, err := cacheNetworkMapFromEvent(&cfg, event)
	if err != nil {
		return err
	}
	if err := client.SaveConfig(*configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("%s map revision %d\n", mapStreamActionWithCache(event.Type, cacheAction), networkMap.Network.Revision)
	return nil
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	output := fs.String("output", "", "write wg-quick configuration to this file; defaults to stdout")
	listenPort := fs.Int("listen-port", 0, "WireGuard listen port included in the exported configuration")
	relayDataplaneEndpoint := fs.String("relay-dataplane-endpoint", "", "local host:base-port used by an explicitly managed relay bridge")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	networkMap, err := verifiedCachedNetworkMap(&cfg)
	if err != nil {
		return err
	}
	rendered, err := renderWireGuardForConfig(cfg, networkMap, *listenPort, *relayDataplaneEndpoint)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*output) == "" {
		fmt.Print(rendered)
		return nil
	}
	if err := client.WriteFileAtomic(*output, []byte(rendered), 0o600); err != nil {
		return err
	}
	fmt.Printf("exported wg-quick configuration to %s\n", *output)
	return nil
}

func parseOptionalDuration(name, value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative duration", name)
	}
	return parsed, nil
}

func updateExitLANPolicyFromFlag(fs *flag.FlagSet, value string, cfg *client.Config) error {
	if !flagWasSet(fs, "exit-lan-policy") {
		return nil
	}
	policy, err := client.NormalizeExitLANPolicy(value)
	if err != nil {
		return err
	}
	cfg.ExitLANPolicy = policy
	return nil
}

func updateWireGuardMTUFromFlag(fs *flag.FlagSet, value int, cfg *client.Config) error {
	if !flagWasSet(fs, "mtu") {
		return nil
	}
	mtu, err := client.NormalizeWireGuardMTU(value)
	if err != nil {
		return err
	}
	cfg.WireGuardMTU = mtu
	return nil
}

func updateWireGuardRouteTableFromFlag(fs *flag.FlagSet, value string, cfg *client.Config) error {
	if !flagWasSet(fs, "route-table") {
		return nil
	}
	routeTable, err := client.NormalizeWireGuardRouteTable(value)
	if err != nil {
		return err
	}
	cfg.WireGuardRouteTable = routeTable
	return nil
}

func cmdDNS(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("dns command requires resolve")
	}
	switch args[0] {
	case "resolve":
		return cmdDNSResolve(args[1:])
	case "serve":
		return cmdDNSServe(args[1:])
	default:
		return fmt.Errorf("unknown dns subcommand %q", args[0])
	}
}

func cmdDNSResolve(args []string) error {
	fs := flag.NewFlagSet("dns resolve", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	name := fs.String("name", "", "DNS name to resolve from the cached signed network map")
	domain := fs.String("domain", "", "DNS search domain; defaults to <network>.endlessnet")
	recordType := fs.String("type", "A", "DNS record type: A or AAAA")
	maxCacheAgeValue := fs.String("max-cache-age", "0s", "maximum accepted cached map age; 0 disables the age check")
	jsonOutput := fs.Bool("json", false, "write resolution as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(*name)
	if fs.NArg() > 0 {
		if query != "" || fs.NArg() != 1 {
			return fmt.Errorf("dns resolve accepts at most one positional name")
		}
		query = strings.TrimSpace(fs.Arg(0))
	}
	if query == "" {
		return fmt.Errorf("dns resolve requires --name or a positional name")
	}
	maxCacheAge, err := parseOptionalDuration("max-cache-age", *maxCacheAgeValue)
	if err != nil {
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	networkMap, err := verifiedCachedNetworkMapWithMaxAge(&cfg, maxCacheAge)
	if err != nil {
		return err
	}
	resolution, err := client.ResolvePeerDNSName(networkMap, query, *domain, client.DNSAddressFamily(strings.ToUpper(strings.TrimSpace(*recordType))))
	if err != nil {
		return err
	}
	if *jsonOutput {
		raw, err := json.MarshalIndent(resolution, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	fmt.Println(resolution.Address)
	return nil
}

func cmdDNSServe(args []string) error {
	fs := flag.NewFlagSet("dns serve", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	listenAddr := fs.String("listen", "127.0.0.1:5353", "UDP address for the local DNS proxy")
	upstreamAddr := fs.String("upstream", "", "default upstream DNS server host:port for public names")
	domain := fs.String("domain", "", "EndlessNet DNS search domain; defaults to <network>.endlessnet")
	maxCacheAgeValue := fs.String("max-cache-age", "0s", "maximum accepted cached map age; 0 disables the age check")
	timeoutValue := fs.String("timeout", "2s", "upstream DNS query timeout")
	splits := multiFlag{}
	fs.Var(&splits, "split", "split DNS rule domain=upstream; empty upstream keeps the domain fail-closed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	maxCacheAge, err := parseOptionalDuration("max-cache-age", *maxCacheAgeValue)
	if err != nil {
		return err
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		return fmt.Errorf("timeout must be a positive duration")
	}
	rules, err := parseSplitDNSRules(splits)
	if err != nil {
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	networkMap, err := verifiedCachedNetworkMapWithMaxAge(&cfg, maxCacheAge)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return client.ServeDNSProxy(ctx, client.DNSProxyOptions{
		ListenAddr:   *listenAddr,
		UpstreamAddr: *upstreamAddr,
		SplitRules:   rules,
		NetworkMap:   networkMap,
		SearchDomain: *domain,
		Timeout:      timeout,
		Ready: func(addr string) {
			fmt.Printf("dns proxy listening on %s\n", addr)
		},
	})
}

func parseSplitDNSRules(values []string) ([]client.SplitDNSRule, error) {
	rules := make([]client.SplitDNSRule, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		domain, upstream, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("split DNS rule %q must use domain=upstream", value)
		}
		domain = strings.TrimSpace(domain)
		if domain == "" {
			return nil, fmt.Errorf("split DNS rule %q has empty domain", value)
		}
		rules = append(rules, client.SplitDNSRule{Domain: domain, Upstream: strings.TrimSpace(upstream)})
	}
	return rules, nil
}

func cmdDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	markNodeOfflineBestEffort(cfg)
	fmt.Println("down completed")
	return nil
}

func markNodeOfflineBestEffort(cfg client.Config) {
	if _, err := publishNodeOffline(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: skipped marking node offline: %v\n", err)
	}
}

func markNodeOfflineConfigBestEffort(configPath string) {
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		log.Printf("warning: skipped marking node offline: %v", err)
		return
	}
	changed, err := publishNodeOffline(&cfg)
	if err != nil {
		log.Printf("warning: skipped marking node offline: %v", err)
		return
	}
	if changed {
		if err := client.SaveConfig(configPath, cfg); err != nil {
			log.Printf("warning: failed to save offline node status: %v", err)
		}
	}
}

func publishNodeOffline(cfg *client.Config) (bool, error) {
	if cfg == nil {
		return false, errors.New("client config is required")
	}
	if strings.TrimSpace(cfg.NodeID) == "" || len(cfg.ControlURLs()) == 0 {
		return false, nil
	}
	if strings.TrimSpace(cfg.Token) == "" && strings.TrimSpace(cfg.NodeCredential) == "" {
		return false, nil
	}
	api := apiFromConfig(*cfg)
	if err := refreshMapSigningTrust(cfg, api); err != nil {
		return false, err
	}
	_, changed, err := updatePublishedEndpointRequestFromConfig(cfg, api, clientapi.UpdateNodeEndpointRequest{Status: clientapi.NodeStatusOffline})
	return changed, err
}

func cmdLogout(args []string) error {
	fs := flag.NewFlagSet("logout", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	stateOutput := fs.String("state-output", "", "agent state JSON file written by agent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := client.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if len(cfg.ControlURLs()) == 0 || (strings.TrimSpace(cfg.Token) == "" && strings.TrimSpace(cfg.NodeCredential) == "") {
		return fmt.Errorf("not logged in; run endlessnet-client login first")
	}
	if strings.TrimSpace(cfg.NodeID) == "" && strings.TrimSpace(cfg.NodeCredential) != "" {
		return fmt.Errorf("node_id is required to revoke node credential")
	}
	if strings.TrimSpace(cfg.NodeCredential) != "" {
		if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
			return err
		}
	}
	api := apiFromConfig(cfg)
	if strings.TrimSpace(cfg.NodeID) != "" {
		if err := api.DeleteNode(cfg.NodeID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.Token) != "" {
		managementAPI, err := managementAPIFromConfig(cfg)
		if err != nil {
			return err
		}
		if err := managementAPI.Logout(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(*stateOutput) != "" {
		if err := removeLogoutFile(*stateOutput); err != nil {
			saveErr := client.SaveConfig(*configPath, client.Config{LocalOwnerID: cfg.LocalOwnerID})
			if saveErr != nil {
				return fmt.Errorf("%v; additionally failed to clear local config: %w", err, saveErr)
			}
			return err
		}
	}
	if err := client.SaveConfig(*configPath, client.Config{LocalOwnerID: cfg.LocalOwnerID}); err != nil {
		return err
	}
	fmt.Println("logout completed")
	return nil
}

func removeLogoutFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("%s already absent\n", path)
			return nil
		}
		return err
	}
	fmt.Printf("removed %s\n", path)
	return nil
}

func resolveNetworkFlag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultNetworkName
	}
	return value
}

func setNetworkRef(req *clientapi.RegisterNodeRequest, network string) {
	if strings.HasPrefix(strings.TrimSpace(network), "net_") {
		req.NetworkID = strings.TrimSpace(network)
		return
	}
	req.NetworkName = strings.TrimSpace(network)
}

func setJoinTokenNetworkRef(req *clientapi.CreateJoinTokenRequest, network string) {
	if strings.HasPrefix(strings.TrimSpace(network), "net_") {
		req.NetworkID = strings.TrimSpace(network)
		return
	}
	req.NetworkName = strings.TrimSpace(network)
}

func refreshMapSigningTrust(cfg *client.Config, api *clientapi.API) error {
	trusted, err := client.SigningTrustBundle(*cfg)
	if err != nil {
		return err
	}
	serverKey, err := api.ServerKey()
	if err != nil {
		return fmt.Errorf("fetch server signing trust bundle: %w", err)
	}
	announced := serverKey.TrustBundle
	if err := announced.Validate(); err != nil {
		return fmt.Errorf("invalid server signing trust bundle: %w", err)
	}
	serverActive, err := announced.Resolve(announced.ActiveKeyID, time.Now().UTC())
	if err != nil {
		return err
	}
	local, err := trusted.Resolve(serverActive.KeyID, time.Now().UTC())
	if err != nil || local.PublicKey != serverActive.PublicKey {
		return errors.New(serverMapSigningTrustChangedError)
	}
	return client.SetSigningTrustBundle(cfg, announced)
}

func enrollMapSigningTrust(cfg *client.Config, api *clientapi.API) error {
	if client.HasSigningTrust(*cfg) {
		return refreshMapSigningTrust(cfg, api)
	}
	if err := validateMapSigningEnrollmentURLs(*cfg); err != nil {
		return err
	}
	serverKey, err := api.ServerKey()
	if err != nil {
		return fmt.Errorf("fetch server signing trust bundle during enrollment: %w", err)
	}
	bundle := serverKey.TrustBundle
	if err := bundle.Validate(); err != nil {
		return fmt.Errorf("invalid server signing trust bundle: %w", err)
	}
	return client.SetSigningTrustBundle(cfg, bundle)
}

func validateMapSigningEnrollmentURLs(cfg client.Config) error {
	for _, rawURL := range cfg.ControlURLs() {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Hostname() == "" {
			return fmt.Errorf("invalid control-plane URL %q", rawURL)
		}
		if strings.EqualFold(parsed.Scheme, "https") {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		addr, _ := netip.ParseAddr(host)
		if !strings.EqualFold(parsed.Scheme, "http") || (host != "localhost" && (!addr.IsValid() || !addr.IsLoopback())) {
			return errors.New("map signing enrollment requires HTTPS; plaintext HTTP is allowed only on loopback")
		}
	}
	return nil
}

func applyMapSigningTrustOption(cfg *client.Config, trustFile string) error {
	trustFile = strings.TrimSpace(trustFile)
	if trustFile == "" {
		return nil
	}
	bundle, err := client.LoadSigningTrustFile(trustFile)
	if err != nil {
		return err
	}
	return client.ReplaceSigningTrustBundle(cfg, bundle)
}

func validateRegistrationResponseBinding(cfg client.Config, req clientapi.RegisterNodeRequest, response clientapi.RegisterNodeResponse, publicKey, identityPublicKey, deviceFingerprint string) error {
	if err := validateNetworkMapBoundary(response); err != nil {
		return err
	}
	if err := validateNodeIdentityBinding(response.Node, publicKey, identityPublicKey, deviceFingerprint); err != nil {
		return fmt.Errorf("registration response: %w", err)
	}
	expectedBinding := clientapi.RegistrationIdentityProofBinding(req)
	if strings.TrimSpace(response.RegistrationBinding) != expectedBinding {
		return errors.New("registration response does not match the signed enrollment request")
	}
	if localNodeID := strings.TrimSpace(cfg.NodeID); localNodeID != "" && response.Node.ID != localNodeID {
		return fmt.Errorf("registration response node_id %q does not match local node_id %q", response.Node.ID, localNodeID)
	}
	if localNetworkID := strings.TrimSpace(cfg.NetworkID); localNetworkID != "" && response.Network.ID != localNetworkID {
		return fmt.Errorf("registration response network_id %q does not match local network_id %q", response.Network.ID, localNetworkID)
	}
	// A join token authoritatively selects its account/network on the server.
	// Direct enrollment must return the network reference the client signed.
	if strings.TrimSpace(req.JoinToken) == "" {
		if requestedID := strings.TrimSpace(req.NetworkID); requestedID != "" && response.Network.ID != requestedID {
			return fmt.Errorf("registration response network_id %q does not match requested network_id %q", response.Network.ID, requestedID)
		}
		if requestedName := strings.TrimSpace(req.NetworkName); requestedName != "" && !strings.EqualFold(strings.TrimSpace(response.Network.Name), requestedName) {
			return fmt.Errorf("registration response network name %q does not match requested network name %q", response.Network.Name, requestedName)
		}
		if requestedAccount := strings.TrimSpace(req.AccountID); requestedAccount != "" && strings.TrimSpace(response.Network.AccountID) != requestedAccount {
			return fmt.Errorf("registration response account_id %q does not match requested account_id %q", response.Network.AccountID, requestedAccount)
		}
	}
	return nil
}

func validateNodeIdentityBinding(node clientapi.Node, publicKey, identityPublicKey, deviceFingerprint string) error {
	if expected := strings.TrimSpace(publicKey); expected == "" || strings.TrimSpace(node.PublicKey) != expected {
		return errors.New("node does not match local WireGuard public key")
	}
	if expected := strings.TrimSpace(identityPublicKey); expected == "" || strings.TrimSpace(node.IdentityPublicKey) != expected {
		return errors.New("node does not match local identity public key")
	}
	if expected := strings.TrimSpace(deviceFingerprint); expected == "" || strings.TrimSpace(node.DeviceFingerprint) != expected {
		return errors.New("node does not match local device fingerprint")
	}
	return nil
}

func verifyRegistrationNodeCredential(api *clientapi.API, response clientapi.RegisterNodeResponse, credential string) error {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return errors.New("registration response is missing node credential")
	}
	serverKey, err := api.ServerKey()
	if err != nil {
		return fmt.Errorf("fetch node credential signing trust bundle: %w", err)
	}
	trust, err := serverKey.NodeCredentialSigningTrustBundle()
	if err != nil {
		return fmt.Errorf("invalid node credential signing trust bundle: %w", err)
	}
	claims, err := clientapi.VerifyNodeCredentialWithTrustBundle(credential, trust, "node:map", time.Now().UTC())
	if err != nil {
		return fmt.Errorf("invalid registration node credential: %w", err)
	}
	if claims.NodeID != strings.TrimSpace(response.Node.ID) {
		return fmt.Errorf("registration node credential node_id %q does not match response node_id %q", claims.NodeID, response.Node.ID)
	}
	if claims.NetworkID != strings.TrimSpace(response.Network.ID) {
		return fmt.Errorf("registration node credential network_id %q does not match response network_id %q", claims.NetworkID, response.Network.ID)
	}
	return nil
}

func verifyNetworkMap(cfg *client.Config, response clientapi.RegisterNodeResponse) error {
	if err := validateNetworkMapBoundary(response); err != nil {
		return err
	}
	if localNodeID := strings.TrimSpace(cfg.NodeID); localNodeID != "" && response.Node.ID != localNodeID {
		return fmt.Errorf("signed network map node_id %q does not match local node_id %q", response.Node.ID, localNodeID)
	}
	if localNetworkID := strings.TrimSpace(cfg.NetworkID); localNetworkID != "" && response.Network.ID != localNetworkID {
		return fmt.Errorf("signed network map network_id %q does not match local network_id %q", response.Network.ID, localNetworkID)
	}
	if strings.TrimSpace(cfg.DeviceFingerprint) != "" {
		publicKey, err := wgkeys.PublicKey(cfg.PrivateKey)
		if err != nil {
			return fmt.Errorf("derive local WireGuard public key: %w", err)
		}
		identityPublicKey, err := client.IdentityPublicKey(cfg.IdentityPrivateKey)
		if err != nil {
			return fmt.Errorf("derive local identity public key: %w", err)
		}
		if err := validateNodeIdentityBinding(response.Node, publicKey, identityPublicKey, cfg.DeviceFingerprint); err != nil {
			return fmt.Errorf("signed network map: %w", err)
		}
	}
	if response.MapSignature == nil {
		return fmt.Errorf("network map signature is missing")
	}
	trust, err := client.SigningTrustBundle(*cfg)
	if err != nil {
		return err
	}
	return clientapi.VerifyNetworkMapSignatureWithTrustBundle(response, trust)
}

func updatePublishedEndpointFromConfig(cfg *client.Config, api *clientapi.API, endpoint string) (clientapi.RegisterNodeResponse, bool, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return clientapi.RegisterNodeResponse{}, false, nil
	}
	return updatePublishedEndpointRequestFromConfig(cfg, api, clientapi.UpdateNodeEndpointRequest{Endpoint: endpoint})
}

func updatePublishedEndpointRequestFromConfig(cfg *client.Config, api *clientapi.API, req clientapi.UpdateNodeEndpointRequest) (clientapi.RegisterNodeResponse, bool, error) {
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Candidates = cleanEndpointCandidates(req.Candidates)
	if req.Endpoint == "" && len(req.Candidates) > 0 {
		req.Endpoint = req.Candidates[0]
	}
	if req.Endpoint == "" && strings.TrimSpace(req.Status) == "" {
		return clientapi.RegisterNodeResponse{}, false, nil
	}
	req.ClientVersion = strings.TrimSpace(version)
	if strings.TrimSpace(cfg.NodeID) == "" || strings.TrimSpace(cfg.NodeCredential) == "" {
		return clientapi.RegisterNodeResponse{}, false, fmt.Errorf("node identity is missing; run up first")
	}
	if err := client.ValidateConfigCurrentDevice(*cfg); err != nil {
		return clientapi.RegisterNodeResponse{}, false, err
	}
	if strings.TrimSpace(req.Status) == "" && req.Generation == 0 && len(req.Candidates) == 0 && cfg.CachedMap != nil && strings.TrimSpace(cfg.CachedMap.Node.Endpoint) == req.Endpoint {
		return clientapi.RegisterNodeResponse{}, false, nil
	}
	response, err := api.UpdateNodeEndpointState(cfg.NodeID, req)
	if err != nil {
		return clientapi.RegisterNodeResponse{}, false, err
	}
	if err := verifyNetworkMap(cfg, response); err != nil {
		return clientapi.RegisterNodeResponse{}, false, err
	}
	if err := cacheNetworkMapChecked(cfg, response); err != nil {
		return clientapi.RegisterNodeResponse{}, false, err
	}
	return response, true, nil
}

func validateNetworkMapBoundary(response clientapi.RegisterNodeResponse) error {
	return clientapi.ValidateNetworkMap(response)
}

func cacheNetworkMap(cfg *client.Config, response clientapi.RegisterNodeResponse) {
	cached := response
	cached.NodeCredential = ""
	cached.RelayCredential = nil
	cfg.NodeID = cached.Node.ID
	cfg.NetworkID = cached.Network.ID
	cfg.NodeApprovalState = strings.ToLower(strings.TrimSpace(cached.Node.ApprovalState))
	if cfg.NodeApprovalState == "" {
		cfg.NodeApprovalState = clientapi.NodeApprovalApproved
	}
	if cfg.NodeApprovalState != clientapi.NodeApprovalPending {
		cfg.EnrollmentRequestID = ""
		cfg.EnrollmentPollToken = ""
		cfg.ApprovalURL = ""
	}
	cfg.MapRevision = cached.Network.Revision
	cfg.CachedMap = &cached
	now := time.Now().UTC()
	cfg.CachedMapSavedAt = &now
}

func cacheNetworkMapChecked(cfg *client.Config, response clientapi.RegisterNodeResponse) error {
	if err := validateNetworkMapBoundary(response); err != nil {
		return err
	}
	if cfg.MapRevision != 0 && response.Network.Revision < cfg.MapRevision {
		return fmt.Errorf("stale network map revision %d is older than local map_revision %d", response.Network.Revision, cfg.MapRevision)
	}
	cacheNetworkMap(cfg, response)
	return nil
}

func cacheNetworkMapFromEvent(cfg *client.Config, event clientapi.MapStreamEvent) (clientapi.RegisterNodeResponse, string, error) {
	current := clientapi.NetworkMapSnapshot{}
	if cfg.CachedMap != nil {
		current = networkMapSnapshotFromResponse(*cfg.CachedMap)
		current.Revision.Network = cfg.MapRevision
		current.Revision.Global = cfg.MapGlobalRevision
		current.MapSignature = mapSignatureForHash(current.MapSignature, cfg.MapHash)
	}
	trust, err := client.SigningTrustBundle(*cfg)
	if err != nil {
		return clientapi.RegisterNodeResponse{}, "", err
	}
	next, err := clientapi.ApplyMapStreamEvent(current, event, trust, time.Now().UTC())
	if errors.Is(err, clientapi.ErrMapStreamEventAlreadyApplied) {
		return networkMapResponseFromSnapshot(next), "unchanged", nil
	}
	if err != nil {
		return clientapi.RegisterNodeResponse{}, "", err
	}
	response := networkMapResponseFromSnapshot(next)
	if err := cacheNetworkMapChecked(cfg, response); err != nil {
		return clientapi.RegisterNodeResponse{}, "", err
	}
	cfg.MapRevision = next.Revision.Network
	cfg.MapGlobalRevision = next.Revision.Global
	cfg.MapHash = ""
	if next.MapSignature != nil {
		cfg.MapHash = next.MapSignature.PayloadHash
	}
	action := "full"
	if event.Type == "delta" {
		action = "delta"
	}
	return response, action, nil
}

func mapStreamCursor(cfg client.Config, networkRevision uint64) clientapi.MapCursor {
	cursor := clientapi.MapCursor{Revision: clientapi.MapRevision{Network: networkRevision}}
	if cfg.CachedMap != nil && cfg.CachedMap.Network.Revision == networkRevision {
		cursor.Revision.Global = cfg.MapGlobalRevision
		cursor.MapHash = cfg.MapHash
		if cursor.MapHash == "" && cfg.CachedMap.MapSignature != nil {
			cursor.MapHash = cfg.CachedMap.MapSignature.PayloadHash
		}
	}
	return cursor
}

func networkMapSnapshotFromResponse(response clientapi.RegisterNodeResponse) clientapi.NetworkMapSnapshot {
	return clientapi.NetworkMapSnapshot{Revision: clientapi.MapRevision{Network: response.Network.Revision}, Network: response.Network, Node: response.Node, Peers: append([]clientapi.Peer(nil), response.Peers...), RegistrationBinding: response.RegistrationBinding, STUNEndpoints: append([]clientapi.STUNEndpoint(nil), response.STUNEndpoints...), Relays: append([]relayauth.Endpoint(nil), response.Relays...), RelayCredential: response.RelayCredential, MapSignature: response.MapSignature}
}

func networkMapResponseFromSnapshot(snapshot clientapi.NetworkMapSnapshot) clientapi.RegisterNodeResponse {
	return clientapi.RegisterNodeResponse{Revision: snapshot.Revision, Network: snapshot.Network, Node: snapshot.Node, Peers: append([]clientapi.Peer(nil), snapshot.Peers...), RegistrationBinding: snapshot.RegistrationBinding, STUNEndpoints: append([]clientapi.STUNEndpoint(nil), snapshot.STUNEndpoints...), Relays: append([]relayauth.Endpoint(nil), snapshot.Relays...), RelayCredential: snapshot.RelayCredential, MapSignature: snapshot.MapSignature}
}

func mapSignatureForHash(signature *clientapi.MapSignature, hash string) *clientapi.MapSignature {
	if signature == nil && hash == "" {
		return nil
	}
	if signature == nil {
		return &clientapi.MapSignature{PayloadHash: hash}
	}
	copy := *signature
	if copy.PayloadHash == "" {
		copy.PayloadHash = hash
	}
	return &copy
}
func verifiedCachedNetworkMap(cfg *client.Config) (clientapi.RegisterNodeResponse, error) {
	return verifiedCachedNetworkMapWithMaxAge(cfg, 0)
}

func verifiedCachedNetworkMapWithMaxAge(cfg *client.Config, maxAge time.Duration) (clientapi.RegisterNodeResponse, error) {
	if cfg.CachedMap == nil {
		return clientapi.RegisterNodeResponse{}, fmt.Errorf("cached network map is missing; run sync or up while online first")
	}
	cached := *cfg.CachedMap
	cached.NodeCredential = ""
	cached.RelayCredential = nil
	if strings.TrimSpace(cfg.NodeID) != "" && cached.Node.ID != cfg.NodeID {
		return clientapi.RegisterNodeResponse{}, fmt.Errorf("cached network map node does not match local node_id")
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		return clientapi.RegisterNodeResponse{}, fmt.Errorf("local node_id is missing")
	}
	if strings.TrimSpace(cfg.NetworkID) == "" {
		return clientapi.RegisterNodeResponse{}, fmt.Errorf("local network_id is missing")
	}
	if cached.Network.ID != cfg.NetworkID {
		return clientapi.RegisterNodeResponse{}, fmt.Errorf("cached network map network_id %q does not match local network_id %q", cached.Network.ID, cfg.NetworkID)
	}
	if cfg.MapRevision != 0 && cached.Network.Revision != cfg.MapRevision {
		return clientapi.RegisterNodeResponse{}, fmt.Errorf("cached network map revision %d does not match local map_revision %d", cached.Network.Revision, cfg.MapRevision)
	}
	if maxAge > 0 {
		if cfg.CachedMapSavedAt == nil || cfg.CachedMapSavedAt.IsZero() {
			return clientapi.RegisterNodeResponse{}, fmt.Errorf("cached network map timestamp is missing")
		}
		now := time.Now().UTC()
		savedAt := cfg.CachedMapSavedAt.UTC()
		if savedAt.After(now.Add(5 * time.Minute)) {
			return clientapi.RegisterNodeResponse{}, fmt.Errorf("cached network map timestamp is in the future")
		}
		if age := now.Sub(savedAt); age > maxAge {
			return clientapi.RegisterNodeResponse{}, fmt.Errorf("cached network map expired: age %s exceeds max-cache-age %s", age.Round(time.Second), maxAge)
		}
	}
	if err := verifyNetworkMap(cfg, cached); err != nil {
		return clientapi.RegisterNodeResponse{}, err
	}
	return cached, nil
}

func renderWireGuardForConfig(cfg client.Config, response clientapi.RegisterNodeResponse, listenPort int, relayDataplaneEndpoint string) (string, error) {
	exitBlockLAN, err := client.ExitLANPolicyBlocksLocalLAN(cfg.ExitLANPolicy)
	if err != nil {
		return "", err
	}
	opts := client.WireGuardRenderOptions{
		ListenPort:       listenPort,
		MTU:              cfg.WireGuardMTU,
		RouteTable:       cfg.WireGuardRouteTable,
		Interfaces:       client.LocalInterfaceStatuses(),
		SubnetRouterSNAT: cfg.SubnetRouterSNAT,
		ExitBlockLAN:     exitBlockLAN,
	}
	if strings.TrimSpace(relayDataplaneEndpoint) != "" {
		overrides, err := client.RelayDataplaneEndpointOverrides(response.Peers, relayDataplaneEndpoint)
		if err != nil {
			return "", err
		}
		opts.PeerEndpointOverrides = overrides
	}
	return client.RenderWireGuardWithOptionsChecked(cfg.PrivateKey, response, opts)
}

func cmdRelayCheck(args []string) error {
	fs := flag.NewFlagSet("relay-check", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	timeoutValue := fs.String("timeout", "5s", "overall relay check timeout")
	watchDurationValue := fs.String("watch-duration", "", "watch relay heartbeats and return after a reconnect or duration elapses")
	heartbeatIntervalValue := fs.String("heartbeat-interval", "1s", "relay heartbeat interval used with --watch-duration")
	relayCAFile := fs.String("relay-ca-cert", "", "PEM CA certificate used to verify TLS relay endpoints")
	jsonOutput := fs.Bool("json", false, "write relay check result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		err := fmt.Errorf("timeout must be a positive duration")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidTimeout, err)
		}
		return err
	}
	watchDuration := time.Duration(0)
	if strings.TrimSpace(*watchDurationValue) != "" {
		watchDuration, err = time.ParseDuration(strings.TrimSpace(*watchDurationValue))
		if err != nil || watchDuration <= 0 {
			err := fmt.Errorf("watch-duration must be a positive duration")
			if *jsonOutput {
				return writeJSONErrorPayload(cliErrorInvalidTimeout, err)
			}
			return err
		}
	}
	heartbeatInterval := time.Second
	if watchDuration > 0 {
		heartbeatInterval, err = time.ParseDuration(strings.TrimSpace(*heartbeatIntervalValue))
		if err != nil || heartbeatInterval <= 0 {
			err := fmt.Errorf("heartbeat-interval must be a positive duration")
			if *jsonOutput {
				return writeJSONErrorPayload(cliErrorInvalidTimeout, err)
			}
			return err
		}
	}
	_, networkMap, err := freshNodeNetworkMap(*configPath, timeout)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorNetworkMapUnavailable, err)
		}
		return err
	}
	tlsConfig, err := relayTLSConfig(*relayCAFile)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorRelayTLSConfigInvalid, err)
		}
		return err
	}
	var payload relayCheckPayload
	if watchDuration > 0 {
		payload, err = relayCheckWatchPayloadFromMap(networkMap, timeout, watchDuration, heartbeatInterval, tlsConfig)
	} else {
		payload, err = relayCheckPayloadFromMap(networkMap, timeout, tlsConfig)
	}
	if *jsonOutput {
		raw, marshalErr := json.MarshalIndent(payload, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(raw))
	} else if payload.Reconnected && payload.Initial != nil && payload.Selected != nil {
		fmt.Printf("relay reconnected: %s %s -> %s %s\n", payload.Initial.ID, payload.Initial.Addr, payload.Selected.ID, payload.Selected.Addr)
	} else if payload.Selected != nil {
		fmt.Printf("relay ok: %s %s (%s)\n", payload.Selected.ID, payload.Selected.Addr, relayProtocol(payload.Selected.Protocol))
	}
	if err != nil {
		return err
	}
	return nil
}

func cmdRelayBridge(args []string) error {
	fs := flag.NewFlagSet("relay-bridge", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	relayDataplaneEndpoint := fs.String("relay-dataplane-endpoint", "", "local host:base-port matching the rendered WireGuard peer endpoints")
	wgListen := fs.String("wg-listen", "", "local WireGuard UDP listen host:port that receives inbound relay datagrams")
	timeoutValue := fs.String("timeout", "5s", "control-plane and relay dial timeout")
	relayCAFile := fs.String("relay-ca-cert", "", "PEM CA certificate used to verify TLS relay endpoints")
	jsonOutput := fs.Bool("json", false, "write the ready status as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*relayDataplaneEndpoint) == "" {
		return fmt.Errorf("relay-dataplane-endpoint is required")
	}
	if strings.TrimSpace(*wgListen) == "" {
		return fmt.Errorf("wg-listen is required")
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		return fmt.Errorf("timeout must be a positive duration")
	}
	tlsConfig, err := relayTLSConfig(*relayCAFile)
	if err != nil {
		return err
	}
	cfg, networkMap, _, err := agentNetworkMap(*configPath, timeout, false, 0, 0)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		return fmt.Errorf("node identity is missing; run up first")
	}
	if networkMap.RelayCredential == nil {
		return fmt.Errorf("relay credential is missing from network map")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return client.RunRelayDataplaneBridge(ctx, client.RelayDataplaneBridgeOptions{
		NetworkMap:          networkMap,
		LocalEndpointBase:   *relayDataplaneEndpoint,
		WireGuardListenAddr: *wgListen,
		Timeout:             timeout,
		TLSConfig:           tlsConfig,
		Ready: func(status client.RelayDataplaneBridgeStatus) {
			if *jsonOutput {
				_ = json.NewEncoder(os.Stdout).Encode(status)
				return
			}
			selected := ""
			if status.Relay.Selected != nil {
				selected = status.Relay.Selected.Addr
			}
			fmt.Printf("relay bridge ready via %s for %d peers\n", selected, len(status.PeerEndpoints))
		},
	})
}

type relayCheckPayload struct {
	OK          bool                      `json:"ok"`
	ErrorCode   string                    `json:"error_code,omitempty"`
	Initial     *relayauth.Endpoint       `json:"initial,omitempty"`
	Selected    *relayauth.Endpoint       `json:"selected"`
	Reconnected bool                      `json:"reconnected,omitempty"`
	Switched    bool                      `json:"switched,omitempty"`
	Attempts    []client.RelayDialAttempt `json:"attempts"`
	Events      []client.RelayWatchEvent  `json:"events,omitempty"`
	DurationMS  float64                   `json:"duration_ms,omitempty"`
	Error       string                    `json:"error,omitempty"`
}

func relayCheckPayloadFromMap(networkMap clientapi.RegisterNodeResponse, timeout time.Duration, tlsConfig *tls.Config) (relayCheckPayload, error) {
	if len(networkMap.Relays) == 0 {
		err := fmt.Errorf("network map does not contain relay endpoints")
		return relayCheckPayload{Attempts: []client.RelayDialAttempt{}, ErrorCode: cliErrorRelayEndpointsMissing, Error: err.Error()}, err
	}
	if networkMap.RelayCredential == nil {
		err := fmt.Errorf("relay credential is missing from network map")
		return relayCheckPayload{Attempts: []client.RelayDialAttempt{}, ErrorCode: cliErrorRelayCredentialMissing, Error: err.Error()}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, result, err := client.DialRelay(ctx, networkMap.Relays, *networkMap.RelayCredential, client.RelayDialOptions{
		Timeout:   timeout,
		TLSConfig: tlsConfig,
	})
	if conn != nil {
		_ = conn.Close()
	}
	payload := relayCheckPayload{
		OK:         err == nil,
		Selected:   result.Selected,
		Attempts:   result.Attempts,
		DurationMS: durationMS(result.Duration),
	}
	if err != nil {
		payload.ErrorCode = relayErrorCode(err)
		payload.Error = err.Error()
	}
	return payload, err
}

func relayCheckWatchPayloadFromMap(networkMap clientapi.RegisterNodeResponse, timeout, watchDuration, heartbeatInterval time.Duration, tlsConfig *tls.Config) (relayCheckPayload, error) {
	if len(networkMap.Relays) == 0 {
		err := fmt.Errorf("network map does not contain relay endpoints")
		return relayCheckPayload{Attempts: []client.RelayDialAttempt{}, ErrorCode: cliErrorRelayEndpointsMissing, Error: err.Error()}, err
	}
	if networkMap.RelayCredential == nil {
		err := fmt.Errorf("relay credential is missing from network map")
		return relayCheckPayload{Attempts: []client.RelayDialAttempt{}, ErrorCode: cliErrorRelayCredentialMissing, Error: err.Error()}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), watchDuration)
	defer cancel()
	result, err := client.WatchRelayFailover(ctx, networkMap.Relays, *networkMap.RelayCredential, client.RelayDialOptions{
		Timeout:           timeout,
		TLSConfig:         tlsConfig,
		HeartbeatInterval: heartbeatInterval,
	})
	payload := relayCheckPayload{
		OK:          err == nil,
		Initial:     result.Initial,
		Selected:    result.Current,
		Reconnected: result.Reconnected,
		Switched:    result.Switched,
		Attempts:    result.Attempts,
		Events:      result.Events,
		DurationMS:  durationMS(result.Duration),
	}
	if err != nil {
		payload.ErrorCode = relayErrorCode(err)
		payload.Error = err.Error()
	}
	return payload, err
}

func cmdPathCheck(args []string) error {
	fs := flag.NewFlagSet("path-check", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	timeoutValue := fs.String("timeout", "5s", "overall path check timeout")
	relayCAFile := fs.String("relay-ca-cert", "", "PEM CA certificate used to verify TLS relay endpoints")
	wgInterface := fs.String("wg-interface", "", "live WireGuard interface to inspect for direct paths")
	wgCommand := fs.String("wg-command", "", "wg command path used when wg-interface is set")
	ipCommand := fs.String("ip-command", "", "ip command path used when wg-interface is set")
	pingCommand := fs.String("ping-command", "", "ping command path used when probe-rtt is set")
	probeRTT := fs.Bool("probe-rtt", false, "probe direct path RTT with ping; requires wg-interface")
	jsonOutput := fs.Bool("json", false, "write path check result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		err := fmt.Errorf("timeout must be a positive duration")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidTimeout, err)
		}
		return err
	}
	if *probeRTT && strings.TrimSpace(*wgInterface) == "" {
		err := fmt.Errorf("wg-interface is required when probe-rtt is set")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorWGInterfaceRequired, err)
		}
		return err
	}
	_, networkMap, err := freshNodeNetworkMap(*configPath, timeout)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorNetworkMapUnavailable, err)
		}
		return err
	}
	probe, err := probePeerPaths(networkMap, networkMap.Peers, pathProbeOptions{
		Timeout:     timeout,
		RelayCAFile: *relayCAFile,
		WGInterface: *wgInterface,
		WGCommand:   *wgCommand,
		IPCommand:   *ipCommand,
		PingCommand: *pingCommand,
		ProbeRTT:    *probeRTT,
	})
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorPathProbeFailed, err)
		}
		return err
	}
	ok := probe.RelayErr == nil || allPeerPathsSelected(probe.Paths)
	payload := map[string]any{
		"ok":    ok,
		"relay": map[string]any{"selected": probe.RelayResult.Selected, "attempts": probe.RelayResult.Attempts},
		"peers": probe.Paths,
	}
	if probe.DirectInspection != nil {
		payload["wireguard"] = *probe.DirectInspection
	}
	if probe.RelayErr != nil && !ok {
		payload["error_code"] = firstNonEmpty(relayErrorCode(probe.RelayErr), cliErrorPathUnavailable)
		payload["error"] = probe.RelayErr.Error()
	}
	if *jsonOutput {
		raw, marshalErr := json.MarshalIndent(payload, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(raw))
	} else {
		for _, path := range probe.Paths {
			fmt.Printf("%s\t%s\t%s\n", path.PeerID, path.Hostname, path.SelectedPath)
		}
	}
	if probe.RelayErr != nil && !ok {
		return probe.RelayErr
	}
	return nil
}

func cmdPing(args []string) error {
	fs := flag.NewFlagSet("ping", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	timeoutValue := fs.String("timeout", "5s", "overall ping timeout")
	relayCAFile := fs.String("relay-ca-cert", "", "PEM CA certificate used to verify TLS relay endpoints")
	wgInterface := fs.String("wg-interface", "", "live WireGuard interface to inspect and probe for direct paths")
	wgCommand := fs.String("wg-command", "", "wg command path used when wg-interface is set")
	ipCommand := fs.String("ip-command", "", "ip command path used when wg-interface is set")
	pingCommand := fs.String("ping-command", "", "ping command path used when wg-interface is set")
	jsonOutput := fs.Bool("json", false, "write ping result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		err := fmt.Errorf("ping requires exactly one peer target")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidArguments, err)
		}
		return err
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		err := fmt.Errorf("timeout must be a positive duration")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidTimeout, err)
		}
		return err
	}
	target := strings.TrimSpace(fs.Arg(0))
	if target == "" {
		err := fmt.Errorf("ping target is required")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidArguments, err)
		}
		return err
	}
	_, networkMap, err := freshNodeNetworkMap(*configPath, timeout)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorNetworkMapUnavailable, err)
		}
		return err
	}
	peer, err := findPeerByPingTarget(networkMap.Peers, target)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorPeerTargetInvalid, err)
		}
		return err
	}
	probe, err := probePeerPaths(networkMap, []clientapi.Peer{peer}, pathProbeOptions{
		Timeout:     timeout,
		RelayCAFile: *relayCAFile,
		WGInterface: *wgInterface,
		WGCommand:   *wgCommand,
		IPCommand:   *ipCommand,
		PingCommand: *pingCommand,
		ProbeRTT:    strings.TrimSpace(*wgInterface) != "",
	})
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorPathProbeFailed, err)
		}
		return err
	}
	if len(probe.Paths) != 1 {
		err := fmt.Errorf("ping path probe returned %d peer paths, want 1", len(probe.Paths))
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorPathProbeFailed, err)
		}
		return err
	}
	payload := pingPayloadFromPath(target, peer, probe.Paths[0], probe.RelayResult, probe.RelayErr, probe.DirectInspection)
	unreachableErr := fmt.Errorf("peer %s is unreachable: direct=%s relay=%s", target, payload.Path.Direct.State, payload.Path.Relay.State)
	if !payload.OK {
		payload.ErrorCode = cliErrorPeerUnreachable
		payload.Error = unreachableErr.Error()
	}
	if *jsonOutput {
		raw, marshalErr := json.MarshalIndent(payload, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(raw))
	} else {
		latency := "-"
		if payload.LatencyMS > 0 {
			latency = fmt.Sprintf("%.3fms", payload.LatencyMS)
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", firstNonEmpty(payload.Peer.Hostname, payload.Peer.ID), payload.SelectedPath, latency, firstNonEmpty(payload.Endpoint, "-"))
	}
	if !payload.OK {
		return unreachableErr
	}
	return nil
}

type pathProbeOptions struct {
	Timeout     time.Duration
	RelayCAFile string
	WGInterface string
	WGCommand   string
	IPCommand   string
	PingCommand string
	ProbeRTT    bool
}

type pathProbeResult struct {
	RelayResult      client.RelayDialResult
	RelayErr         error
	DirectInspection *client.WireGuardInspection
	RTTProbes        map[string]client.DirectRTTProbe
	Paths            []client.PeerPathStatus
}

func probePeerPaths(networkMap clientapi.RegisterNodeResponse, peers []clientapi.Peer, opts pathProbeOptions) (pathProbeResult, error) {
	result := pathProbeResult{}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if networkMap.RelayCredential == nil {
		result.RelayErr = fmt.Errorf("relay credential is missing from network map")
	} else {
		tlsConfig, err := relayTLSConfig(opts.RelayCAFile)
		if err != nil {
			return result, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		conn, relayResult, err := client.DialRelay(ctx, networkMap.Relays, *networkMap.RelayCredential, client.RelayDialOptions{
			Timeout:   timeout,
			TLSConfig: tlsConfig,
		})
		result.RelayResult = relayResult
		result.RelayErr = err
		if conn != nil {
			_ = conn.Close()
		}
	}
	if strings.TrimSpace(opts.WGInterface) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		routeTargets := client.WireGuardRouteTargetsForPeers(peers)
		inspection := client.InspectWireGuard(ctx, client.WireGuardInspectOptions{
			Interface:    opts.WGInterface,
			RouteTargets: routeTargets,
			WGCommand:    opts.WGCommand,
			IPCommand:    opts.IPCommand,
		})
		result.DirectInspection = &inspection
		if opts.ProbeRTT {
			result.RTTProbes = make(map[string]client.DirectRTTProbe, len(routeTargets))
			for _, target := range routeTargets {
				result.RTTProbes[target] = client.ProbeDirectRTT(ctx, client.DirectRTTProbeOptions{
					Target:      target,
					PingCommand: opts.PingCommand,
				})
			}
		}
	}
	probeMap := networkMap
	probeMap.Peers = append([]clientapi.Peer(nil), peers...)
	result.Paths = client.BuildPathStatusesWithDirectProbes(probeMap, result.RelayResult, result.RelayErr, result.DirectInspection, result.RTTProbes)
	return result, nil
}

type pingPeerSummary struct {
	ID          string   `json:"id"`
	Hostname    string   `json:"hostname"`
	RouteTarget string   `json:"route_target,omitempty"`
	AllowedIPs  []string `json:"allowed_ips,omitempty"`
}

type pingRelaySummary struct {
	Selected   *relayauth.Endpoint       `json:"selected,omitempty"`
	Attempts   []client.RelayDialAttempt `json:"attempts,omitempty"`
	DurationMS float64                   `json:"duration_ms,omitempty"`
	Error      string                    `json:"error,omitempty"`
}

type pingPayload struct {
	OK           bool                        `json:"ok"`
	ErrorCode    string                      `json:"error_code,omitempty"`
	Error        string                      `json:"error,omitempty"`
	Target       string                      `json:"target"`
	Peer         pingPeerSummary             `json:"peer"`
	SelectedPath string                      `json:"selected_path"`
	LatencyMS    float64                     `json:"latency_ms,omitempty"`
	Endpoint     string                      `json:"endpoint,omitempty"`
	Path         client.PeerPathStatus       `json:"path"`
	Relay        pingRelaySummary            `json:"relay"`
	WireGuard    *client.WireGuardInspection `json:"wireguard,omitempty"`
}

func pingPayloadFromPath(target string, peer clientapi.Peer, path client.PeerPathStatus, relayResult client.RelayDialResult, relayErr error, inspection *client.WireGuardInspection) pingPayload {
	payload := pingPayload{
		OK:           path.SelectedPath != "" && path.SelectedPath != "none",
		Target:       target,
		Peer:         pingPeerSummary{ID: peer.ID, Hostname: peer.Hostname, RouteTarget: client.PeerRouteTarget(peer), AllowedIPs: append([]string(nil), peer.AllowedIPs...)},
		SelectedPath: path.SelectedPath,
		Path:         path,
		Relay: pingRelaySummary{
			Selected:   relayResult.Selected,
			Attempts:   relayResult.Attempts,
			DurationMS: durationMS(relayResult.Duration),
		},
		WireGuard: inspection,
	}
	if relayErr != nil {
		payload.Relay.Error = relayErr.Error()
	}
	switch path.SelectedPath {
	case "direct":
		payload.Endpoint = path.Direct.Endpoint
		payload.LatencyMS = path.Direct.RTTMS
	case "relay":
		payload.Endpoint = path.Relay.Endpoint
		payload.LatencyMS = durationMS(relayResult.Duration)
	}
	return payload
}

func durationMS(duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	ms := float64(duration) / float64(time.Millisecond)
	if ms > 0 && ms < 0.001 {
		return 0.001
	}
	return ms
}

func findPeerByPingTarget(peers []clientapi.Peer, target string) (clientapi.Peer, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return clientapi.Peer{}, fmt.Errorf("ping target is required")
	}
	matches := make([]clientapi.Peer, 0, 1)
	for _, peer := range peers {
		if peerMatchesPingTarget(peer, target) {
			matches = append(matches, peer)
		}
	}
	switch len(matches) {
	case 0:
		return clientapi.Peer{}, fmt.Errorf("peer %q was not found in the signed network map", target)
	case 1:
		return matches[0], nil
	default:
		return clientapi.Peer{}, fmt.Errorf("peer target %q is ambiguous", target)
	}
}

func peerMatchesPingTarget(peer clientapi.Peer, target string) bool {
	if peer.ID == target || strings.EqualFold(peer.Hostname, target) {
		return true
	}
	targetAddr, err := netip.ParseAddr(target)
	if err != nil {
		return false
	}
	for _, routeTarget := range peerOverlayRouteTargets(peer) {
		if routeTarget == targetAddr {
			return true
		}
	}
	return false
}

func peerOverlayRouteTargets(peer clientapi.Peer) []netip.Addr {
	out := []netip.Addr{}
	for _, allowed := range peer.AllowedIPs {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(allowed); err == nil {
			if prefix.Bits() == prefix.Addr().BitLen() {
				out = append(out, prefix.Addr())
			}
			continue
		}
		if addr, err := netip.ParseAddr(allowed); err == nil {
			out = append(out, addr)
		}
	}
	return out
}

func allPeerPathsSelected(paths []client.PeerPathStatus) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if strings.TrimSpace(path.SelectedPath) == "" || path.SelectedPath == "none" {
			return false
		}
	}
	return true
}

func cmdNetcheck(args []string) error {
	fs := flag.NewFlagSet("netcheck", flag.ExitOnError)
	configPath := fs.String("config", "", "client config path")
	timeoutValue := fs.String("timeout", "2s", "per-STUN endpoint timeout")
	relayTimeoutValue := fs.String("relay-timeout", "2s", "overall relay check timeout")
	relayCAFile := fs.String("relay-ca-cert", "", "PEM CA certificate used to verify TLS relay endpoints")
	natPMPGateway := fs.String("nat-pmp-gateway", "", "NAT-PMP gateway host:port to request a UDP port mapping")
	pcpServer := fs.String("pcp-server", "", "PCP server host:port to request a UDP MAP")
	upnpControlURL := fs.String("upnp-control-url", "", "UPnP IGD WANIPConnection control URL to request and clean up a UDP port mapping")
	portMapPort := fs.Int("port-map-port", 0, "local UDP port to map through NAT-PMP/PCP/UPnP")
	portMapExternalPort := fs.Int("port-map-external-port", 0, "requested external UDP port for NAT-PMP/PCP/UPnP; 0 lets the gateway choose")
	portMapLifetimeValue := fs.String("port-map-lifetime", "2m", "requested NAT-PMP/PCP/UPnP mapping lifetime")
	jsonOutput := fs.Bool("json", false, "write netcheck result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(*timeoutValue))
	if err != nil || timeout <= 0 {
		err := fmt.Errorf("timeout must be a positive duration")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidTimeout, err)
		}
		return err
	}
	relayTimeout, err := time.ParseDuration(strings.TrimSpace(*relayTimeoutValue))
	if err != nil || relayTimeout <= 0 {
		err := fmt.Errorf("relay-timeout must be a positive duration")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidRelayTimeout, err)
		}
		return err
	}
	portMapLifetime, err := time.ParseDuration(strings.TrimSpace(*portMapLifetimeValue))
	if err != nil || portMapLifetime <= 0 {
		err := fmt.Errorf("port-map-lifetime must be a positive duration")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidArguments, err)
		}
		return err
	}
	portMappingRequested := strings.TrimSpace(*natPMPGateway) != "" || strings.TrimSpace(*pcpServer) != "" || strings.TrimSpace(*upnpControlURL) != ""
	if portMappingRequested && (*portMapPort <= 0 || *portMapPort > 65535) {
		err := fmt.Errorf("port-map-port must be between 1 and 65535 when NAT-PMP, PCP, or UPnP mapping is requested")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidArguments, err)
		}
		return err
	}
	if *portMapExternalPort < 0 || *portMapExternalPort > 65535 {
		err := fmt.Errorf("port-map-external-port must be between 0 and 65535")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorInvalidArguments, err)
		}
		return err
	}
	_, networkMap, err := freshNodeNetworkMap(*configPath, timeout)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorNetworkMapUnavailable, err)
		}
		return err
	}
	if len(networkMap.STUNEndpoints) == 0 {
		err := fmt.Errorf("network map does not contain STUN endpoints")
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorSTUNEndpointsMissing, err)
		}
		return err
	}
	tlsConfig, err := relayTLSConfig(*relayCAFile)
	if err != nil {
		if *jsonOutput {
			return writeJSONErrorPayload(cliErrorRelayTLSConfigInvalid, err)
		}
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Duration(len(networkMap.STUNEndpoints))+time.Second)
	defer cancel()
	results := client.CheckSTUN(ctx, networkMap.STUNEndpoints, timeout)
	nat := client.ClassifySTUN(results)
	relay, _ := relayCheckPayloadFromMap(networkMap, relayTimeout, tlsConfig)
	portMappings := runPortMappingChecks(context.Background(), *natPMPGateway, *pcpServer, *upnpControlURL, *portMapPort, *portMapExternalPort, portMapLifetime, timeout)
	interfaces := client.LocalInterfaceStatuses()
	routeConflicts := client.OverlayCIDRConflicts(networkMap, interfaces)
	portMappingsOK := portMappingResultsOK(portMappings)
	ok := nat.ReachableEndpoints > 0 && len(routeConflicts) == 0 && portMappingsOK
	payload := map[string]any{
		"ok":                   ok,
		"interfaces":           interfaces,
		"route_conflict_count": len(routeConflicts),
		"route_conflicts":      routeConflicts,
		"stun":                 results,
		"nat":                  nat,
		"relay":                relay,
		"port_mappings":        portMappings,
	}
	stunErr := fmt.Errorf("no STUN endpoint returned a mapped address")
	routeConflictErr := routeConflictError(routeConflicts)
	if routeConflictErr != nil {
		payload["error_code"] = cliErrorRouteConflict
		payload["error"] = routeConflictErr.Error()
	} else if !portMappingsOK {
		payload["error_code"] = cliErrorPathUnavailable
		payload["error"] = portMappingError(portMappings)
	} else if !ok {
		payload["error_code"] = cliErrorSTUNUnreachable
		payload["error"] = stunErr.Error()
	}
	if *jsonOutput {
		raw, marshalErr := json.MarshalIndent(payload, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Println(string(raw))
	} else {
		for _, result := range results {
			state := "failed"
			if result.Reachable {
				state = "ok"
			}
			fmt.Printf("%s\t%s\t%s\t%s\n", result.ID, result.Addr, state, result.MappedAddress)
		}
		fmt.Printf("nat\t%s\t%d/%d\t%s\n", nat.Classification, nat.ReachableEndpoints, nat.TotalEndpoints, strings.Join(nat.MappedAddresses, ","))
		if relay.Selected != nil {
			fmt.Printf("relay\tok\t%s\t%.3fms\n", relay.Selected.Addr, relay.DurationMS)
		} else if relay.Error != "" {
			fmt.Printf("relay\tfailed\t%s\n", relay.Error)
		}
		for _, mapping := range portMappings {
			state := "failed"
			if mapping.OK {
				state = "ok"
			}
			fmt.Printf("port_mapping\t%s\t%s\t%s\t%s\n", mapping.Protocol, mapping.Gateway, state, mapping.MappedEndpoint)
		}
	}
	if !ok {
		if routeConflictErr != nil {
			return routeConflictErr
		}
		if !portMappingsOK {
			return fmt.Errorf("%s", portMappingError(portMappings))
		}
		return stunErr
	}
	return nil
}

func runPortMappingChecks(ctx context.Context, natPMPGateway, pcpServer, upnpControlURL string, internalPort, externalPort int, lifetime, timeout time.Duration) []client.PortMappingResult {
	results := []client.PortMappingResult{}
	req := client.PortMappingRequest{
		InternalPort: internalPort,
		ExternalPort: externalPort,
		Lifetime:     lifetime,
		Timeout:      timeout,
	}
	if gateway := strings.TrimSpace(natPMPGateway); gateway != "" {
		req.Gateway = gateway
		results = append(results, client.MapNATPMP(ctx, req))
	}
	if server := strings.TrimSpace(pcpServer); server != "" {
		req.Gateway = server
		results = append(results, client.MapPCP(ctx, req))
	}
	if controlURL := strings.TrimSpace(upnpControlURL); controlURL != "" {
		req.Gateway = controlURL
		results = append(results, client.MapUPnP(ctx, req))
	}
	return results
}

func portMappingResultsOK(results []client.PortMappingResult) bool {
	for _, result := range results {
		if !result.OK {
			return false
		}
	}
	return true
}

func portMappingError(results []client.PortMappingResult) string {
	for _, result := range results {
		if strings.TrimSpace(result.Error) != "" {
			return fmt.Sprintf("%s port mapping failed: %s", result.Protocol, result.Error)
		}
	}
	return "port mapping failed"
}

func freshNodeNetworkMap(configPath string, timeout time.Duration) (client.Config, clientapi.RegisterNodeResponse, error) {
	return freshNodeNetworkMapFromRevision(configPath, timeout, 0)
}

func freshNodeNetworkMapFromRevision(configPath string, timeout time.Duration, fromRevision uint64) (client.Config, clientapi.RegisterNodeResponse, error) {
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, err
	}
	if len(cfg.ControlURLs()) == 0 {
		return cfg, clientapi.RegisterNodeResponse{}, fmt.Errorf("server URL is required; run up or login first")
	}
	if strings.TrimSpace(cfg.NodeID) == "" || strings.TrimSpace(cfg.NodeCredential) == "" {
		return cfg, clientapi.RegisterNodeResponse{}, fmt.Errorf("node identity is missing; run up first")
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, err
	}
	api := apiFromConfig(cfg)
	api.HTTPClient.Timeout = timeout + 5*time.Second
	if err := refreshMapSigningTrust(&cfg, api); err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, err
	}
	event, err := api.ReadMapStreamEvent(cfg.NodeID, mapStreamCursor(cfg, fromRevision), timeout)
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, err
	}
	networkMap, _, err := cacheNetworkMapFromEvent(&cfg, event)
	if err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, err
	}
	if err := client.SaveConfig(configPath, cfg); err != nil {
		return cfg, clientapi.RegisterNodeResponse{}, err
	}
	return cfg, networkMap, nil
}

func serviceIPCStatusForConfig(cfg client.Config) (response ipc.StatusResponse) {
	approvalState := strings.ToLower(strings.TrimSpace(cfg.NodeApprovalState))
	response = ipc.StatusResponse{
		ControlState:              ipc.ControlStateNotRegistered,
		DesiredState:              ipc.DesiredConnected,
		ControlPlaneURLs:          append([]string(nil), cfg.ControlURLs()...),
		AccountID:                 cfg.ActiveAccountID,
		NodeID:                    cfg.NodeID,
		NetworkID:                 cfg.NetworkID,
		EnrollmentRequestID:       strings.TrimSpace(cfg.EnrollmentRequestID),
		ApprovalURL:               strings.TrimSpace(cfg.ApprovalURL),
		NodeApprovalState:         approvalState,
		MapRevision:               cfg.MapRevision,
		MapSigningTrustPresent:    client.HasSigningTrust(cfg),
		TokenPresent:              strings.TrimSpace(cfg.Token) != "",
		NodeCredentialPresent:     strings.TrimSpace(cfg.NodeCredential) != "",
		DeviceFingerprintPresent:  strings.TrimSpace(cfg.DeviceFingerprint) != "",
		IdentityPrivateKeyPresent: strings.TrimSpace(cfg.IdentityPrivateKey) != "",
		PrivateKeyPresent:         strings.TrimSpace(cfg.PrivateKey) != "",
		CachedMapPresent:          cfg.CachedMap != nil,
		RouteTable:                strings.TrimSpace(cfg.WireGuardRouteTable),
		STUNEndpoints:             []ipc.EndpointAddress{},
		RelayEndpoints:            []ipc.RelayEndpoint{},
		Agent: &ipc.AgentStatus{
			StatePresent:  false,
			SnapshotState: ipc.AgentSnapshotAbsent,
		},
	}
	defer func() {
		response.State = serviceStateFromControlState(response.ControlState, response.CachedMapError != "")
	}()
	if strings.TrimSpace(cfg.NodeID) != "" || strings.TrimSpace(cfg.NodeCredential) != "" {
		response.ControlState = ipc.ControlStateRegistered
	}
	if err := client.ValidateConfigCurrentDevice(cfg); err != nil {
		response.ControlState = ipc.ControlStateError
		response.LocalStateError = err.Error()
		return response
	}
	if approvalState == clientapi.NodeApprovalPending {
		response.ControlState = ipc.ControlStatePendingApproval
		return response
	}
	if approvalState == clientapi.NodeApprovalRejected {
		response.ControlState = ipc.ControlStateError
		response.ApprovalError = "node enrollment was rejected"
		return response
	}
	if cfg.CachedMap == nil {
		return response
	}
	cached, err := verifiedCachedNetworkMap(&cfg)
	if err != nil {
		response.CachedMapError = err.Error()
		response.ControlState = ipc.ControlStateCacheInvalid
		return response
	}
	response.CachedMapValid = true
	response.NetworkID = cached.Network.ID
	response.NetworkName = cached.Network.Name
	if strings.TrimSpace(response.AccountID) == "" {
		response.AccountID = cached.Network.AccountID
	}
	response.OverlayCIDR = cached.Network.CIDR
	response.OverlayIPv6CIDR = cached.Network.IPv6CIDR
	response.OverlayIP = cached.Node.AssignedIP
	response.OverlayIPv6 = cached.Node.AssignedIPv6
	response.NodeID = cached.Node.ID
	response.Hostname = cached.Node.Hostname
	response.MapRevision = cached.Network.Revision
	response.PeerCount = len(cached.Peers)
	response.STUNEndpoints = make([]ipc.EndpointAddress, 0, len(cached.STUNEndpoints))
	for _, endpoint := range cached.STUNEndpoints {
		response.STUNEndpoints = append(response.STUNEndpoints, ipc.EndpointAddress{
			ID:   endpoint.ID,
			Addr: endpoint.Addr,
		})
	}
	response.RelayEndpoints = make([]ipc.RelayEndpoint, 0, len(cached.Relays))
	for _, endpoint := range cached.Relays {
		response.RelayEndpoints = append(response.RelayEndpoints, ipc.RelayEndpoint{
			ID:       endpoint.ID,
			Addr:     endpoint.Addr,
			Protocol: relayProtocol(endpoint.Protocol),
			Priority: endpoint.Priority,
		})
	}
	if len(cfg.ControlURLs()) == 0 {
		response.ControlState = ipc.ControlStateOfflineCache
	} else {
		response.ControlState = ipc.ControlStateReady
	}
	return response
}

func statusPayload(cfg client.Config) map[string]any {
	raw, err := json.Marshal(serviceIPCStatusForConfig(cfg))
	if err != nil {
		return map[string]any{"control_state": ipc.ControlStateError, "local_state_error": err.Error()}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{"control_state": ipc.ControlStateError, "local_state_error": err.Error()}
	}
	delete(payload, "ipc_protocol")
	delete(payload, "ipc_version")
	delete(payload, "ipc_min_supported_version")
	delete(payload, "state")
	return payload
}

func attachLocalRouteConflicts(payload map[string]any, cfg client.Config, includeInterfaces bool, ignoredInterfaces ...string) []client.OverlayCIDRConflict {
	interfaces := client.LocalInterfaceStatuses()
	if includeInterfaces {
		payload["interfaces"] = interfaces
	}
	conflicts := []client.OverlayCIDRConflict{}
	if cfg.CachedMap != nil {
		if cached, err := verifiedCachedNetworkMap(&cfg); err == nil {
			conflicts = client.OverlayCIDRConflicts(cached, interfaces, ignoredInterfaces...)
		}
	}
	payload["route_conflict_count"] = len(conflicts)
	payload["route_conflicts"] = conflicts
	return conflicts
}

func routeConflictError(conflicts []client.OverlayCIDRConflict) error {
	if len(conflicts) == 0 {
		return nil
	}
	conflict := conflicts[0]
	return fmt.Errorf("overlay CIDR %s conflicts with local prefix %s on interface %s", conflict.OverlayCIDR, conflict.LocalPrefix, conflict.Interface)
}

func routeConflictErrorForMap(networkMap clientapi.RegisterNodeResponse, ignoredInterfaces ...string) error {
	return routeConflictError(client.OverlayCIDRConflicts(networkMap, client.LocalInterfaceStatuses(), ignoredInterfaces...))
}

func attachControlAvailability(ctx context.Context, payload map[string]any, serverURLs ...string) {
	urls := clientapi.NormalizeControlPlaneURLs(serverURLs...)
	if len(urls) == 0 {
		return
	}
	attempts := make([]map[string]any, 0, len(urls))
	for _, serverURL := range urls {
		control := probeControlReadyz(ctx, serverURL)
		attempts = append(attempts, control)
		if control["ok"] == true {
			if len(attempts) > 1 {
				control["attempts"] = attempts
			}
			payload["control"] = control
			return
		}
	}
	control := attempts[len(attempts)-1]
	if len(attempts) > 1 {
		control["attempts"] = attempts
	}
	payload["control"] = control
	markControlDegraded(payload)
}

func attachServiceIPCControlAvailability(ctx context.Context, status *ipc.StatusResponse, serverURLs ...string) {
	urls := clientapi.NormalizeControlPlaneURLs(serverURLs...)
	if status == nil || len(urls) == 0 {
		return
	}
	attempts := make([]ipc.ControlProbe, 0, len(urls))
	for _, serverURL := range urls {
		probe := probeServiceIPCControlReadyz(ctx, serverURL)
		attempts = append(attempts, probe)
		if probe.OK {
			if len(attempts) > 1 {
				probe.Attempts = append([]ipc.ControlProbe(nil), attempts...)
			}
			status.Control = &probe
			return
		}
	}
	probe := attempts[len(attempts)-1]
	if len(attempts) > 1 {
		probe.Attempts = append([]ipc.ControlProbe(nil), attempts...)
	}
	status.Control = &probe
	markServiceIPCControlDegraded(status)
}

func probeServiceIPCControlReadyz(ctx context.Context, serverURL string) ipc.ControlProbe {
	readyURL := strings.TrimRight(strings.TrimSpace(serverURL), "/") + "/client/readyz"
	probe := ipc.ControlProbe{URL: readyURL}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	probe.HTTPStatus = resp.StatusCode
	probe.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !probe.OK {
		probe.Error = resp.Status
	}
	return probe
}

func markServiceIPCControlDegraded(status *ipc.StatusResponse) {
	if status == nil {
		return
	}
	switch status.ControlState {
	case ipc.ControlStateReady, ipc.ControlStateRegistered:
		status.ControlState = ipc.ControlStateDegraded
		return
	}
	if status.CachedMapValid {
		status.ControlState = ipc.ControlStateDegraded
	}
}

func probeControlReadyz(ctx context.Context, serverURL string) map[string]any {
	readyURL := strings.TrimRight(strings.TrimSpace(serverURL), "/") + "/client/readyz"
	control := map[string]any{
		"ok":  false,
		"url": readyURL,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)
	if err != nil {
		control["error"] = err.Error()
		return control
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		control["error"] = err.Error()
		return control
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	control["http_status"] = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		control["ok"] = true
		return control
	}
	control["error"] = resp.Status
	return control
}

func markControlDegraded(payload map[string]any) {
	switch fmt.Sprint(payload["control_state"]) {
	case string(ipc.ControlStateReady), string(ipc.ControlStateRegistered):
		payload["control_state"] = ipc.ControlStateDegraded
		return
	}
	if valid, _ := payload["cached_map_valid"].(bool); valid {
		payload["control_state"] = ipc.ControlStateDegraded
	}
}

func attachAgentStatus(payload map[string]any, snapshot client.AgentSnapshot) {
	selectedRelay := map[string]string{}
	if snapshot.Relay.Selected != nil {
		protocol := strings.TrimSpace(snapshot.Relay.Selected.Protocol)
		if protocol == "" {
			protocol = "tcp"
		}
		selectedRelay = map[string]string{
			"id":       snapshot.Relay.Selected.ID,
			"addr":     snapshot.Relay.Selected.Addr,
			"protocol": protocol,
		}
	}
	selectedPathCounts := map[string]int{}
	for _, path := range snapshot.Paths {
		selectedPathCounts[path.SelectedPath]++
	}
	agent := map[string]any{
		"state_present":        true,
		"generated_at":         snapshot.GeneratedAt,
		"node_id":              snapshot.NodeID,
		"network_id":           snapshot.NetworkID,
		"network_name":         snapshot.NetworkName,
		"overlay_ip":           snapshot.OverlayIP,
		"map_revision":         snapshot.MapRevision,
		"peer_count":           snapshot.PeerCount,
		"stun_ok":              snapshot.STUN.OK,
		"relay_ok":             snapshot.Relay.OK,
		"selected_relay":       selectedRelay,
		"relay_attempt_count":  len(snapshot.Relay.Attempts),
		"path_count":           len(snapshot.Paths),
		"selected_path_counts": selectedPathCounts,
		"peers":                snapshot.Paths,
	}
	if strings.TrimSpace(snapshot.OverlayIPv6) != "" {
		agent["overlay_ipv6"] = snapshot.OverlayIPv6
	}
	if lastError := strings.TrimSpace(snapshot.LastError); lastError != "" {
		agent["last_error"] = lastError
		if strings.Contains(strings.ToLower(lastError), serverMapSigningTrustChangedError) {
			payload["control_state"] = ipc.ControlStateServerIdentityChanged
			payload["recovery"] = map[string]any{"state": ipc.StateServerIdentityChanged}
		} else if fmt.Sprint(payload["control_state"]) == string(ipc.ControlStatePendingApproval) {
			// A pending enrollment is expected to receive authorization errors
			// until an administrator approves it. Preserve the actionable state.
		} else if payload["cached_map_error"] != nil {
			payload["control_state"] = ipc.ControlStateError
		} else if valid, _ := payload["cached_map_valid"].(bool); valid {
			payload["control_state"] = ipc.ControlStateDegraded
		} else if fmt.Sprint(payload["control_state"]) == string(ipc.ControlStateNotRegistered) {
			payload["control_state"] = ipc.ControlStateNotRegistered
		} else {
			payload["control_state"] = ipc.ControlStateError
		}
	}
	payload["agent"] = agent
}

func attachServiceIPCAgentStatus(status *ipc.StatusResponse, snapshot client.AgentSnapshot) {
	if status == nil {
		return
	}
	snapshotState, ok := agentSnapshotStateForStatus(*status, snapshot)
	if !ok {
		return
	}
	selectedRelay := ipc.RelayEndpoint{}
	if snapshot.Relay.Selected != nil {
		selectedRelay = ipc.RelayEndpoint{
			ID:       snapshot.Relay.Selected.ID,
			Addr:     snapshot.Relay.Selected.Addr,
			Protocol: relayProtocol(snapshot.Relay.Selected.Protocol),
			Priority: snapshot.Relay.Selected.Priority,
		}
	}
	selectedPathCounts := map[string]int{}
	for _, path := range snapshot.Paths {
		selectedPathCounts[path.SelectedPath]++
	}
	status.Agent = &ipc.AgentStatus{
		StatePresent:       true,
		SnapshotState:      snapshotState,
		GeneratedAt:        snapshot.GeneratedAt,
		NodeID:             snapshot.NodeID,
		NetworkID:          snapshot.NetworkID,
		NetworkName:        snapshot.NetworkName,
		OverlayIP:          snapshot.OverlayIP,
		OverlayIPv6:        snapshot.OverlayIPv6,
		MapRevision:        snapshot.MapRevision,
		PeerCount:          snapshot.PeerCount,
		STUNOK:             snapshot.STUN.OK,
		RelayOK:            snapshot.Relay.OK,
		SelectedRelay:      selectedRelay,
		RelayAttemptCount:  len(snapshot.Relay.Attempts),
		PathCount:          len(snapshot.Paths),
		SelectedPathCounts: selectedPathCounts,
		Peers:              peerPathStatuses(snapshot.Paths),
		LastError:          strings.TrimSpace(snapshot.LastError),
	}
	if snapshotState == ipc.AgentSnapshotPrevious {
		status.Agent.TargetMapRevision = status.MapRevision
	}
	if status.Agent.LastError == "" {
		return
	}
	if strings.Contains(strings.ToLower(status.Agent.LastError), serverMapSigningTrustChangedError) {
		status.ControlState = ipc.ControlStateServerIdentityChanged
		status.Recovery = &ipc.RecoveryStatus{State: ipc.StateServerIdentityChanged}
		return
	}
	if status.ControlState == ipc.ControlStatePendingApproval {
		return
	}
	if status.CachedMapError != "" {
		status.ControlState = ipc.ControlStateError
	} else if status.CachedMapValid {
		status.ControlState = ipc.ControlStateDegraded
	} else if status.ControlState != ipc.ControlStateNotRegistered {
		status.ControlState = ipc.ControlStateError
	}
}

func attachLiveWireGuardStatus(payload map[string]any, cfg client.Config, wgInterface string, routeTargets []string, probeRTT bool, timeout time.Duration, relayResult client.RelayDialResult, relayErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cachedMap *clientapi.RegisterNodeResponse
	inspectTargets := append([]string(nil), routeTargets...)
	if cfg.CachedMap != nil {
		if cached, err := verifiedCachedNetworkMap(&cfg); err == nil {
			cachedMap = &cached
			inspectTargets = appendUniqueStrings(inspectTargets, client.WireGuardRouteTargetsForPeers(cached.Peers))
		}
	}
	inspection := client.InspectWireGuard(ctx, client.WireGuardInspectOptions{
		Interface:    wgInterface,
		RouteTargets: inspectTargets,
	})
	payload["wireguard"] = inspection
	if cachedMap == nil {
		return
	}
	var rttProbes map[string]client.DirectRTTProbe
	var routeTargetProbes map[string]client.DirectRTTProbe
	if probeRTT {
		peerTargets := client.WireGuardRouteTargetsForPeers(cachedMap.Peers)
		rttProbes = make(map[string]client.DirectRTTProbe, len(peerTargets))
		for _, target := range peerTargets {
			rttProbes[target] = client.ProbeDirectRTT(ctx, client.DirectRTTProbeOptions{Target: target})
		}
		routeTargetProbes = make(map[string]client.DirectRTTProbe, len(routeTargets))
		for _, target := range routeTargets {
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			routeTargetProbes[target] = client.ProbeDirectRTT(ctx, client.DirectRTTProbeOptions{Target: target})
		}
	}
	attachLivePathStatus(payload, *cachedMap, inspection, rttProbes, relayResult, relayErr)
	if diagnostics := subnetRouteDiagnostics(*cachedMap, inspection, routeTargets, probeRTT, rttProbes, routeTargetProbes); len(diagnostics) > 0 {
		payload["subnet_route_diagnostics"] = diagnostics
	}
}

func attachLivePathStatus(payload map[string]any, networkMap clientapi.RegisterNodeResponse, inspection client.WireGuardInspection, rttProbes map[string]client.DirectRTTProbe, relayResult client.RelayDialResult, relayErr error) {
	paths := client.BuildPathStatusesWithDirectProbes(networkMap, relayResult, relayErr, &inspection, rttProbes)
	selectedPathCounts := map[string]int{}
	for _, path := range paths {
		selectedPathCounts[path.SelectedPath]++
	}
	payload["path_count"] = len(paths)
	payload["selected_path_counts"] = selectedPathCounts
	payload["paths"] = paths
}

type subnetRouteDiagnostic struct {
	Target              string  `json:"target"`
	Status              string  `json:"status"`
	Reason              string  `json:"reason,omitempty"`
	PeerID              string  `json:"peer_id,omitempty"`
	Hostname            string  `json:"hostname,omitempty"`
	MatchedPrefix       string  `json:"matched_prefix,omitempty"`
	PeerRouteTarget     string  `json:"peer_route_target,omitempty"`
	RouteUsesInterface  bool    `json:"route_uses_interface"`
	PeerReachable       bool    `json:"peer_reachable"`
	TargetReachable     bool    `json:"target_reachable"`
	PeerRTTMS           float64 `json:"peer_rtt_ms,omitempty"`
	TargetRTTMS         float64 `json:"target_rtt_ms,omitempty"`
	PeerProbeError      string  `json:"peer_probe_error,omitempty"`
	TargetProbeError    string  `json:"target_probe_error,omitempty"`
	WireGuardRouteError string  `json:"wireguard_route_error,omitempty"`
}

func subnetRouteDiagnostics(networkMap clientapi.RegisterNodeResponse, inspection client.WireGuardInspection, routeTargets []string, probeRTT bool, peerProbes, targetProbes map[string]client.DirectRTTProbe) []subnetRouteDiagnostic {
	targets := appendUniqueStrings(nil, routeTargets)
	out := make([]subnetRouteDiagnostic, 0, len(targets))
	for _, target := range targets {
		out = append(out, subnetRouteDiagnosticForTarget(networkMap, inspection, target, probeRTT, peerProbes, targetProbes))
	}
	return out
}

func subnetRouteDiagnosticForTarget(networkMap clientapi.RegisterNodeResponse, inspection client.WireGuardInspection, target string, probeRTT bool, peerProbes, targetProbes map[string]client.DirectRTTProbe) subnetRouteDiagnostic {
	diag := subnetRouteDiagnostic{Target: strings.TrimSpace(target)}
	addr, err := netip.ParseAddr(diag.Target)
	if err != nil {
		diag.Status = "invalid_target"
		diag.Reason = "route target is not an IP address"
		return diag
	}
	peer, prefix, ok := signedPeerForRouteTarget(networkMap.Peers, addr)
	if !ok {
		diag.Status = "not_in_signed_map"
		diag.Reason = "route target is not covered by any signed peer allowed_ips"
		return diag
	}
	diag.PeerID = peer.ID
	diag.Hostname = peer.Hostname
	diag.MatchedPrefix = prefix.String()
	diag.PeerRouteTarget = client.PeerRouteTarget(peer)
	route, ok := liveRouteInspectionForTarget(inspection, diag.Target)
	if !ok {
		diag.Status = "route_not_inspected"
		diag.Reason = "route target was not inspected by live WireGuard status"
		return diag
	}
	diag.RouteUsesInterface = route.UsesInterface
	if route.Error != "" {
		diag.Status = "route_error"
		diag.WireGuardRouteError = route.Error
		diag.Reason = route.Error
		return diag
	}
	if !route.UsesInterface {
		diag.Status = "route_not_wireguard"
		diag.Reason = "route target does not use the WireGuard interface"
		return diag
	}
	if prefix.Bits() == prefix.Addr().BitLen() {
		diag.Status = "peer_route"
		diag.Reason = "route target is a peer overlay address, not an advertised subnet target"
		return diag
	}
	if !probeRTT {
		diag.Status = "untested"
		diag.Reason = "return-path diagnosis requires --probe-rtt"
		return diag
	}
	if diag.PeerRouteTarget == "" {
		diag.Status = "router_unreachable"
		diag.Reason = "subnet router has no peer overlay route target"
		return diag
	}
	peerProbe, ok := peerProbes[diag.PeerRouteTarget]
	if !ok {
		diag.Status = "router_unreachable"
		diag.Reason = "subnet router peer was not probed"
		return diag
	}
	if peerProbe.Error != "" {
		diag.Status = "router_unreachable"
		diag.PeerProbeError = peerProbe.Error
		diag.Reason = "subnet router peer did not respond"
		return diag
	}
	diag.PeerReachable = true
	diag.PeerRTTMS = peerProbe.RTTMS
	targetProbe, ok := targetProbes[diag.Target]
	if !ok {
		diag.Status = "untested"
		diag.Reason = "route target was not probed"
		return diag
	}
	if targetProbe.Error != "" {
		diag.Status = "return_path_issue"
		diag.TargetProbeError = targetProbe.Error
		diag.Reason = "subnet router is reachable and the target route uses WireGuard, but the target did not respond; check the LAN return route or enable SNAT on the subnet router"
		return diag
	}
	diag.TargetReachable = true
	diag.TargetRTTMS = targetProbe.RTTMS
	diag.Status = "reachable"
	return diag
}

func signedPeerForRouteTarget(peers []clientapi.Peer, target netip.Addr) (clientapi.Peer, netip.Prefix, bool) {
	var (
		bestPeer   clientapi.Peer
		bestPrefix netip.Prefix
		found      bool
	)
	for _, peer := range peers {
		for _, allowed := range peer.AllowedIPs {
			prefix, ok := allowedIPPrefix(allowed)
			if !ok || !prefix.Contains(target) {
				continue
			}
			if !found || prefix.Bits() > bestPrefix.Bits() {
				bestPeer = peer
				bestPrefix = prefix
				found = true
			}
		}
	}
	return bestPeer, bestPrefix, found
}

func allowedIPPrefix(allowed string) (netip.Prefix, bool) {
	allowed = strings.TrimSpace(allowed)
	if allowed == "" {
		return netip.Prefix{}, false
	}
	if prefix, err := netip.ParsePrefix(allowed); err == nil {
		return prefix.Masked(), true
	}
	if addr, err := netip.ParseAddr(allowed); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen()), true
	}
	return netip.Prefix{}, false
}

func liveRouteInspectionForTarget(inspection client.WireGuardInspection, target string) (client.WireGuardRouteInspection, bool) {
	for _, route := range inspection.Routes {
		if route.Target == target {
			return route, true
		}
	}
	return client.WireGuardRouteInspection{}, false
}

func probeStatusRelay(networkMap clientapi.RegisterNodeResponse, timeout time.Duration, relayCAFile string) (client.RelayDialResult, error, error) {
	if networkMap.RelayCredential == nil {
		return client.RelayDialResult{}, fmt.Errorf("relay credential is missing from network map"), nil
	}
	tlsConfig, err := relayTLSConfig(relayCAFile)
	if err != nil {
		return client.RelayDialResult{}, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, relayResult, relayErr := client.DialRelay(ctx, networkMap.Relays, *networkMap.RelayCredential, client.RelayDialOptions{
		Timeout:   timeout,
		TLSConfig: tlsConfig,
	})
	if conn != nil {
		_ = conn.Close()
	}
	return relayResult, relayErr, nil
}

func appendUniqueStrings(values []string, extras []string) []string {
	seen := make(map[string]bool, len(values)+len(extras))
	out := make([]string, 0, len(values)+len(extras))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, value := range extras {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func relayTLSConfig(caFile string) (*tls.Config, error) {
	if strings.TrimSpace(caFile) == "" {
		return nil, nil
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	raw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	if !roots.AppendCertsFromPEM(raw) {
		return nil, fmt.Errorf("relay CA file %s does not contain PEM certificates", caFile)
	}
	return client.NewRelayTLSConfig(roots), nil
}

func relayProtocol(protocol string) string {
	protocol = strings.TrimSpace(protocol)
	if protocol == "" {
		return "tcp"
	}
	return protocol
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

func requireLoopbackInlineSessionToken(rawURLs []string) error {
	if len(rawURLs) == 0 {
		return errors.New("server URL is required")
	}
	for _, rawURL := range rawURLs {
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil {
			return fmt.Errorf("invalid control-plane URL %q", rawURL)
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("invalid control-plane URL %q", rawURL)
		}
		host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		addr, _ := netip.ParseAddr(host)
		if host != "localhost" && (!addr.IsValid() || !addr.IsLoopback()) {
			return errors.New("session tokens must not be passed in command arguments for non-loopback servers; use --token-file or --token-file -")
		}
	}
	return nil
}

const maxSecretInputBytes = 16 * 1024

func secretFlagValue(name, inlineValue, filePath string) (string, error) {
	inlineValue = strings.TrimSpace(inlineValue)
	filePath = strings.TrimSpace(filePath)
	if inlineValue != "" && filePath != "" {
		return "", fmt.Errorf("use either --%s or --%s-file, not both", name, name)
	}
	if filePath == "" {
		return inlineValue, nil
	}
	var (
		raw []byte
		err error
	)
	if filePath == "-" {
		raw, err = io.ReadAll(io.LimitReader(os.Stdin, maxSecretInputBytes+1))
	} else {
		var file *os.File
		file, err = os.Open(filePath)
		if err == nil {
			defer func() { _ = file.Close() }()
			var info os.FileInfo
			info, err = file.Stat()
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("%s file %s is not a regular file", name, filePath)
			}
			if err == nil && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
				err = fmt.Errorf("%s file %s must not be readable or writable by group or other users; mode is %04o, want 0600 or stricter", name, filePath, info.Mode().Perm())
			}
			if err == nil {
				raw, err = io.ReadAll(io.LimitReader(file, maxSecretInputBytes+1))
			}
		}
	}
	if err != nil {
		return "", err
	}
	if len(raw) > maxSecretInputBytes {
		return "", fmt.Errorf("%s input exceeds %d bytes", name, maxSecretInputBytes)
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", fmt.Errorf("%s file is empty", name)
	}
	return value, nil
}

func mapStreamAction(eventType string) string {
	if eventType == "resync" {
		return "resynced"
	}
	return "synced"
}

func mapStreamActionWithCache(eventType, cacheAction string) string {
	action := mapStreamAction(eventType)
	if cacheAction == "delta" {
		return "delta-" + action
	}
	return action
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func loadAPI(configPath string) (client.Config, *clientapi.API, error) {
	cfg, err := client.LoadConfig(configPath)
	if err != nil {
		return cfg, nil, err
	}
	if len(cfg.ControlURLs()) == 0 || strings.TrimSpace(cfg.Token) == "" {
		return cfg, nil, fmt.Errorf("not logged in; run endlessnet-client login first")
	}
	return cfg, apiFromConfig(cfg), nil
}

func apiFromConfig(cfg client.Config) *clientapi.API {
	return clientapi.NewAPIWithNodeCredentialURLs(cfg.ControlURLs(), cfg.Token, cfg.NodeCredential)
}

func managementAPIFromConfig(cfg client.Config) (*clientapi.API, error) {
	managementURL := strings.TrimRight(strings.TrimSpace(cfg.ManagementURL), "/")
	parsed, err := url.Parse(managementURL)
	if err != nil || !isSecureOriginURL(parsed) {
		return nil, errors.New("management_url is required and must be a secure origin; run endlessnet-client login")
	}
	return clientapi.NewAPI(managementURL+"/api/v1", cfg.Token), nil
}

func resolveBillingAccount(cfg client.Config, api *clientapi.API, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit), nil
	}
	if strings.TrimSpace(cfg.ActiveAccountID) != "" {
		return strings.TrimSpace(cfg.ActiveAccountID), nil
	}
	accounts, err := api.ListAccounts()
	if err != nil {
		return "", err
	}
	if len(accounts) == 1 {
		return accounts[0].ID, nil
	}
	if len(accounts) == 0 {
		return "", fmt.Errorf("no billing accounts are available")
	}
	return "", fmt.Errorf("multiple billing accounts are available; pass --account or run billing accounts --use <id>")
}

func printJSON(value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func amountLabel(amount int64, currency string) string {
	if amount < 0 {
		return "custom"
	}
	return fmt.Sprintf("%d.%02d %s", amount/100, amount%100, strings.ToUpper(strings.TrimSpace(currency)))
}

func firstControlPlaneURL(cfg client.Config) string {
	urls := cfg.ControlURLs()
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func mustHostname() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "node"
	}
	return hostname
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	*m = append(*m, strings.TrimSpace(value))
	return nil
}

func usage() {
	fmt.Println("usage: endlessnet-client login|logout|keygen|version|network|join-token|billing|nodes|up|sync|export|agent|service|state|down|dns|status|relay-check|path-check|ping|netcheck|diagnostics|relay-bridge")
	fmt.Println("network subcommands: create, list, routes, approve-route, revoke-route")
	fmt.Println("billing subcommands: plans, accounts, summary, checkout, invoices")
	fmt.Println("dns subcommands: resolve, serve")
}

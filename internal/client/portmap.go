package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPortMappingTimeout = 2 * time.Second
	defaultPortMappingLife    = 2 * time.Minute
)

type PortMappingRequest struct {
	Gateway      string
	InternalPort int
	ExternalPort int
	Lifetime     time.Duration
	Timeout      time.Duration
	pcpNonce     []byte
}

type PortMappingResult struct {
	Protocol        string `json:"protocol"`
	Gateway         string `json:"gateway"`
	OK              bool   `json:"ok"`
	InternalPort    int    `json:"internal_port,omitempty"`
	ExternalPort    int    `json:"external_port,omitempty"`
	ExternalAddress string `json:"external_address,omitempty"`
	MappedEndpoint  string `json:"mapped_endpoint,omitempty"`
	LifetimeSeconds int    `json:"lifetime_seconds,omitempty"`
	CleanupOK       bool   `json:"cleanup_ok,omitempty"`
	Error           string `json:"error,omitempty"`
	pcpNonce        []byte
}

const upnpWANIPConnectionService = "urn:schemas-upnp-org:service:WANIPConnection:1"

func MapNATPMP(ctx context.Context, req PortMappingRequest) PortMappingResult {
	result := basePortMappingResult("nat_pmp", req)
	if err := validatePortMappingRequest(req); err != nil {
		result.Error = err.Error()
		return result
	}
	conn, err := dialUDPPortMapper(ctx, req.Gateway, req.Timeout)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer func() { _ = conn.Close() }()
	if err := setPortMapperDeadline(ctx, conn, req.Timeout); err != nil {
		result.Error = err.Error()
		return result
	}
	externalAddress, err := natPMPExternalAddress(conn)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if err := setPortMapperDeadline(ctx, conn, req.Timeout); err != nil {
		result.Error = err.Error()
		return result
	}
	externalPort, lifetimeSeconds, err := natPMPMapUDP(conn, req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.ExternalPort = externalPort
	result.ExternalAddress = externalAddress.String()
	result.LifetimeSeconds = lifetimeSeconds
	result.MappedEndpoint = net.JoinHostPort(result.ExternalAddress, fmt.Sprintf("%d", result.ExternalPort))
	return result
}

func MapPCP(ctx context.Context, req PortMappingRequest) PortMappingResult {
	result := basePortMappingResult("pcp", req)
	if err := validatePortMappingRequest(req); err != nil {
		result.Error = err.Error()
		return result
	}
	conn, err := dialUDPPortMapper(ctx, req.Gateway, req.Timeout)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer func() { _ = conn.Close() }()
	if err := setPortMapperDeadline(ctx, conn, req.Timeout); err != nil {
		result.Error = err.Error()
		return result
	}
	externalAddress, externalPort, lifetimeSeconds, nonce, err := pcpMapUDP(conn, req)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.ExternalPort = externalPort
	result.ExternalAddress = externalAddress.String()
	result.LifetimeSeconds = lifetimeSeconds
	result.MappedEndpoint = net.JoinHostPort(result.ExternalAddress, fmt.Sprintf("%d", result.ExternalPort))
	result.pcpNonce = append([]byte(nil), nonce[:]...)
	return result
}

func MapUPnP(ctx context.Context, req PortMappingRequest) PortMappingResult {
	result := basePortMappingResult("upnp", req)
	if err := validatePortMappingRequest(req); err != nil {
		result.Error = err.Error()
		return result
	}
	controlURL, err := parseUPnPControlURL(req.Gateway)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultPortMappingTimeout
	}
	client := &http.Client{Timeout: timeout}
	internalClient, err := upnpInternalClient(ctx, controlURL, timeout)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	externalPort := req.ExternalPort
	if externalPort == 0 {
		externalPort = req.InternalPort
	}
	result.ExternalPort = externalPort
	externalAddress, err := upnpExternalAddress(ctx, client, controlURL.String())
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if err := validateUPnPExternalAddress(externalAddress); err != nil {
		result.Error = err.Error()
		return result
	}
	added := false
	if err := upnpAddPortMapping(ctx, client, controlURL.String(), req, externalPort, internalClient); err != nil {
		result.Error = err.Error()
		return result
	}
	added = true
	cleanup := func() error {
		if !added {
			return nil
		}
		err := upnpDeletePortMapping(ctx, client, controlURL.String(), externalPort)
		if err == nil {
			result.CleanupOK = true
		}
		return err
	}
	entry, err := upnpSpecificPortMapping(ctx, client, controlURL.String(), externalPort)
	if err != nil {
		cleanupErr := cleanup()
		result.Error = portMappingCleanupError(err, cleanupErr)
		return result
	}
	if err := validateUPnPSpecificPortMapping(entry, req.InternalPort, internalClient); err != nil {
		cleanupErr := cleanup()
		result.Error = portMappingCleanupError(err, cleanupErr)
		return result
	}
	lifetimeSeconds := int(portMappingLifetimeSeconds(req.Lifetime))
	if err := cleanup(); err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.ExternalAddress = externalAddress.String()
	result.LifetimeSeconds = lifetimeSeconds
	result.MappedEndpoint = net.JoinHostPort(result.ExternalAddress, fmt.Sprintf("%d", result.ExternalPort))
	return result
}

func basePortMappingResult(protocol string, req PortMappingRequest) PortMappingResult {
	return PortMappingResult{
		Protocol:     protocol,
		Gateway:      strings.TrimSpace(req.Gateway),
		InternalPort: req.InternalPort,
		ExternalPort: req.ExternalPort,
	}
}

func validatePortMappingRequest(req PortMappingRequest) error {
	if strings.TrimSpace(req.Gateway) == "" {
		return errors.New("port mapping gateway is required")
	}
	if req.InternalPort <= 0 || req.InternalPort > 65535 {
		return errors.New("port mapping internal port must be between 1 and 65535")
	}
	if req.ExternalPort < 0 || req.ExternalPort > 65535 {
		return errors.New("port mapping external port must be between 0 and 65535")
	}
	return nil
}

func parseUPnPControlURL(raw string) (*url.URL, error) {
	controlURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if controlURL.Scheme != "http" && controlURL.Scheme != "https" {
		return nil, errors.New("UPnP control URL must use http or https")
	}
	if strings.TrimSpace(controlURL.Host) == "" {
		return nil, errors.New("UPnP control URL host is required")
	}
	return controlURL, nil
}

func dialUDPPortMapper(ctx context.Context, gateway string, timeout time.Duration) (*net.UDPConn, error) {
	if timeout <= 0 {
		timeout = defaultPortMappingTimeout
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "udp4", strings.TrimSpace(gateway))
	if err != nil {
		return nil, err
	}
	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("port mapping dial did not return UDP connection")
	}
	return udpConn, nil
}

func setPortMapperDeadline(ctx context.Context, conn *net.UDPConn, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultPortMappingTimeout
	}
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	return conn.SetDeadline(deadline)
}

func portMappingLifetimeSeconds(lifetime time.Duration) uint32 {
	if lifetime <= 0 {
		lifetime = defaultPortMappingLife
	}
	seconds := int64(lifetime.Round(time.Second) / time.Second)
	if seconds <= 0 {
		return 1
	}
	if seconds > int64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(seconds)
}

func natPMPExternalAddress(conn *net.UDPConn) (netip.Addr, error) {
	if _, err := conn.Write([]byte{0, 0}); err != nil {
		return netip.Addr{}, err
	}
	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	if err != nil {
		return netip.Addr{}, err
	}
	if n < 12 {
		return netip.Addr{}, fmt.Errorf("short NAT-PMP public address response: %d bytes", n)
	}
	if buf[0] != 0 || buf[1] != 128 {
		return netip.Addr{}, fmt.Errorf("unexpected NAT-PMP public address response opcode %d/%d", buf[0], buf[1])
	}
	if code := binary.BigEndian.Uint16(buf[2:4]); code != 0 {
		return netip.Addr{}, fmt.Errorf("NAT-PMP public address result code %d", code)
	}
	addr, ok := netip.AddrFromSlice(buf[8:12])
	if !ok || !addr.Is4() {
		return netip.Addr{}, errors.New("NAT-PMP public address response did not contain IPv4 address")
	}
	return addr, nil
}

func natPMPMapUDP(conn *net.UDPConn, req PortMappingRequest) (int, int, error) {
	request := make([]byte, 12)
	request[0] = 0
	request[1] = 1
	binary.BigEndian.PutUint16(request[4:6], uint16(req.InternalPort))
	binary.BigEndian.PutUint16(request[6:8], uint16(req.ExternalPort))
	binary.BigEndian.PutUint32(request[8:12], portMappingLifetimeSeconds(req.Lifetime))
	if _, err := conn.Write(request); err != nil {
		return 0, 0, err
	}
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if err != nil {
		return 0, 0, err
	}
	if n < 16 {
		return 0, 0, fmt.Errorf("short NAT-PMP UDP mapping response: %d bytes", n)
	}
	if buf[0] != 0 || buf[1] != 129 {
		return 0, 0, fmt.Errorf("unexpected NAT-PMP UDP mapping response opcode %d/%d", buf[0], buf[1])
	}
	if code := binary.BigEndian.Uint16(buf[2:4]); code != 0 {
		return 0, 0, fmt.Errorf("NAT-PMP UDP mapping result code %d", code)
	}
	if internalPort := int(binary.BigEndian.Uint16(buf[8:10])); internalPort != req.InternalPort {
		return 0, 0, fmt.Errorf("NAT-PMP mapped internal port %d, want %d", internalPort, req.InternalPort)
	}
	externalPort := int(binary.BigEndian.Uint16(buf[10:12]))
	if externalPort <= 0 {
		return 0, 0, errors.New("NAT-PMP mapped external port is empty")
	}
	lifetimeSeconds := int(binary.BigEndian.Uint32(buf[12:16]))
	if lifetimeSeconds <= 0 {
		return 0, 0, errors.New("NAT-PMP mapped lifetime is empty")
	}
	return externalPort, lifetimeSeconds, nil
}

func pcpMapUDP(conn *net.UDPConn, req PortMappingRequest) (netip.Addr, int, int, [12]byte, error) {
	request := make([]byte, 60)
	request[0] = 2
	request[1] = 1
	binary.BigEndian.PutUint32(request[4:8], portMappingLifetimeSeconds(req.Lifetime))
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || localAddr == nil {
		return netip.Addr{}, 0, 0, [12]byte{}, errors.New("PCP local UDP address is unavailable")
	}
	localIP, ok := netip.AddrFromSlice(localAddr.IP)
	if !ok || !localIP.IsValid() {
		return netip.Addr{}, 0, 0, [12]byte{}, errors.New("PCP local IP address is invalid")
	}
	localIP = localIP.Unmap()
	if localIP.Is4() {
		request[18] = 0xff
		request[19] = 0xff
		copy(request[20:24], localIP.AsSlice())
	} else {
		copy(request[8:24], localIP.AsSlice())
	}
	var nonce [12]byte
	if len(req.pcpNonce) == len(nonce) {
		copy(nonce[:], req.pcpNonce)
	} else if _, err := rand.Read(nonce[:]); err != nil {
		return netip.Addr{}, 0, 0, nonce, err
	}
	copy(request[24:36], nonce[:])
	request[36] = 17
	binary.BigEndian.PutUint16(request[40:42], uint16(req.InternalPort))
	binary.BigEndian.PutUint16(request[42:44], uint16(req.ExternalPort))
	if _, err := conn.Write(request); err != nil {
		return netip.Addr{}, 0, 0, nonce, err
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		return netip.Addr{}, 0, 0, nonce, err
	}
	if n < 60 {
		return netip.Addr{}, 0, 0, nonce, fmt.Errorf("short PCP MAP response: %d bytes", n)
	}
	if buf[0] != 2 || buf[1] != 0x81 {
		return netip.Addr{}, 0, 0, nonce, fmt.Errorf("unexpected PCP MAP response opcode %d/%d", buf[0], buf[1])
	}
	if code := buf[3]; code != 0 {
		return netip.Addr{}, 0, 0, nonce, fmt.Errorf("PCP MAP result code %d", code)
	}
	lifetimeSeconds := int(binary.BigEndian.Uint32(buf[4:8]))
	if lifetimeSeconds <= 0 {
		return netip.Addr{}, 0, 0, nonce, errors.New("PCP mapped lifetime is empty")
	}
	if string(buf[24:36]) != string(nonce[:]) {
		return netip.Addr{}, 0, 0, nonce, errors.New("PCP MAP response nonce mismatch")
	}
	if protocol := buf[36]; protocol != 17 {
		return netip.Addr{}, 0, 0, nonce, fmt.Errorf("PCP MAP protocol %d, want UDP", protocol)
	}
	if internalPort := int(binary.BigEndian.Uint16(buf[40:42])); internalPort != req.InternalPort {
		return netip.Addr{}, 0, 0, nonce, fmt.Errorf("PCP mapped internal port %d, want %d", internalPort, req.InternalPort)
	}
	externalPort := int(binary.BigEndian.Uint16(buf[42:44]))
	if externalPort <= 0 {
		return netip.Addr{}, 0, 0, nonce, errors.New("PCP mapped external port is empty")
	}
	addr, ok := netip.AddrFromSlice(buf[44:60])
	if !ok {
		return netip.Addr{}, 0, 0, nonce, errors.New("PCP MAP response did not contain external address")
	}
	if addr.Is4In6() {
		addr = netip.AddrFrom4(addr.As4())
	}
	addr = addr.Unmap()
	if !addr.Is4() && !addr.Is6() {
		return netip.Addr{}, 0, 0, nonce, errors.New("PCP MAP external address is invalid")
	}
	return addr, externalPort, lifetimeSeconds, nonce, nil
}

type upnpPortMappingEntry struct {
	InternalClient string
	InternalPort   int
	Enabled        bool
}

func upnpInternalClient(ctx context.Context, controlURL *url.URL, timeout time.Duration) (string, error) {
	host := controlURL.Host
	if !strings.Contains(host, ":") {
		if controlURL.Scheme == "https" {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	addr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok || addr.IP == nil {
		return "", errors.New("UPnP local client address is unavailable")
	}
	ip := addr.IP.To4()
	if ip == nil {
		return "", errors.New("UPnP local client address must be IPv4")
	}
	return ip.String(), nil
}

func upnpExternalAddress(ctx context.Context, client *http.Client, controlURL string) (netip.Addr, error) {
	raw, err := upnpSOAPAction(ctx, client, controlURL, "GetExternalIPAddress", "")
	if err != nil {
		return netip.Addr{}, err
	}
	value, err := upnpSOAPValue(raw, "NewExternalIPAddress")
	if err != nil {
		return netip.Addr{}, err
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("UPnP external address %q is invalid: %w", value, err)
	}
	return addr.Unmap(), nil
}

func validateUPnPExternalAddress(addr netip.Addr) error {
	if !addr.Is4() {
		return fmt.Errorf("UPnP external address %s is not IPv4", addr)
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return fmt.Errorf("UPnP external address %s is not a safe public unicast address", addr)
	}
	return nil
}

func upnpAddPortMapping(ctx context.Context, client *http.Client, controlURL string, req PortMappingRequest, externalPort int, internalClient string) error {
	body := strings.Join([]string{
		upnpSOAPArg("NewRemoteHost", ""),
		upnpSOAPArg("NewExternalPort", fmt.Sprintf("%d", externalPort)),
		upnpSOAPArg("NewProtocol", "UDP"),
		upnpSOAPArg("NewInternalPort", fmt.Sprintf("%d", req.InternalPort)),
		upnpSOAPArg("NewInternalClient", internalClient),
		upnpSOAPArg("NewEnabled", "1"),
		upnpSOAPArg("NewPortMappingDescription", "EndlessNet"),
		upnpSOAPArg("NewLeaseDuration", fmt.Sprintf("%d", portMappingLifetimeSeconds(req.Lifetime))),
	}, "")
	_, err := upnpSOAPAction(ctx, client, controlURL, "AddPortMapping", body)
	return err
}

func upnpSpecificPortMapping(ctx context.Context, client *http.Client, controlURL string, externalPort int) (upnpPortMappingEntry, error) {
	body := strings.Join([]string{
		upnpSOAPArg("NewRemoteHost", ""),
		upnpSOAPArg("NewExternalPort", fmt.Sprintf("%d", externalPort)),
		upnpSOAPArg("NewProtocol", "UDP"),
	}, "")
	raw, err := upnpSOAPAction(ctx, client, controlURL, "GetSpecificPortMappingEntry", body)
	if err != nil {
		return upnpPortMappingEntry{}, err
	}
	internalClient, err := upnpSOAPValue(raw, "NewInternalClient")
	if err != nil {
		return upnpPortMappingEntry{}, err
	}
	internalPortValue, err := upnpSOAPValue(raw, "NewInternalPort")
	if err != nil {
		return upnpPortMappingEntry{}, err
	}
	internalPort, err := strconv.Atoi(strings.TrimSpace(internalPortValue))
	if err != nil {
		return upnpPortMappingEntry{}, fmt.Errorf("UPnP internal port %q is invalid: %w", internalPortValue, err)
	}
	enabled := true
	if enabledValue, err := upnpSOAPValue(raw, "NewEnabled"); err == nil {
		switch strings.ToLower(strings.TrimSpace(enabledValue)) {
		case "1", "true", "yes":
			enabled = true
		case "0", "false", "no":
			enabled = false
		default:
			return upnpPortMappingEntry{}, fmt.Errorf("UPnP enabled value %q is invalid", enabledValue)
		}
	}
	return upnpPortMappingEntry{
		InternalClient: strings.TrimSpace(internalClient),
		InternalPort:   internalPort,
		Enabled:        enabled,
	}, nil
}

func validateUPnPSpecificPortMapping(entry upnpPortMappingEntry, internalPort int, internalClient string) error {
	if entry.InternalPort != internalPort {
		return fmt.Errorf("UPnP mapped internal port %d, want %d", entry.InternalPort, internalPort)
	}
	gotClient, err := netip.ParseAddr(entry.InternalClient)
	if err != nil {
		return fmt.Errorf("UPnP mapped internal client %q is invalid: %w", entry.InternalClient, err)
	}
	wantClient, err := netip.ParseAddr(internalClient)
	if err != nil {
		return err
	}
	if gotClient.Unmap() != wantClient.Unmap() {
		return fmt.Errorf("UPnP mapped internal client %s, want %s", gotClient.Unmap(), wantClient.Unmap())
	}
	if !entry.Enabled {
		return errors.New("UPnP mapped entry is disabled")
	}
	return nil
}

func upnpDeletePortMapping(ctx context.Context, client *http.Client, controlURL string, externalPort int) error {
	body := strings.Join([]string{
		upnpSOAPArg("NewRemoteHost", ""),
		upnpSOAPArg("NewExternalPort", fmt.Sprintf("%d", externalPort)),
		upnpSOAPArg("NewProtocol", "UDP"),
	}, "")
	_, err := upnpSOAPAction(ctx, client, controlURL, "DeletePortMapping", body)
	return err
}

func upnpSOAPAction(ctx context.Context, client *http.Client, controlURL, action, body string) ([]byte, error) {
	envelope := `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
		`<s:Body><u:` + action + ` xmlns:u="` + upnpWANIPConnectionService + `">` +
		body +
		`</u:` + action + `></s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL, strings.NewReader(envelope))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+upnpWANIPConnectionService+`#`+action+`"`)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if fault, err := upnpSOAPValue(raw, "faultstring"); err == nil && strings.TrimSpace(fault) != "" {
			return nil, fmt.Errorf("UPnP %s failed: %s", action, strings.TrimSpace(fault))
		}
		return nil, fmt.Errorf("UPnP %s failed: HTTP %s", action, resp.Status)
	}
	return raw, nil
}

func upnpSOAPArg(name, value string) string {
	return "<" + name + ">" + xmlText(value) + "</" + name + ">"
}

func xmlText(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

func upnpSOAPValue(raw []byte, name string) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != name {
			continue
		}
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	return "", fmt.Errorf("UPnP SOAP response missing %s", name)
}

func portMappingCleanupError(primary, cleanup error) string {
	if primary == nil && cleanup == nil {
		return ""
	}
	if primary == nil {
		return cleanup.Error()
	}
	if cleanup == nil {
		return primary.Error()
	}
	return fmt.Sprintf("%v; cleanup failed: %v", primary, cleanup)
}

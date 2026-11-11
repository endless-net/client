//go:build windows

package clientapp

import (
	"errors"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/caarlos0/env/v11"
	approvalmodel "github.com/endless-net/client/internal/approval"
	"golang.org/x/sys/windows"
)

var ErrConfig = errors.New("invalid client configuration")

// Config describes the Windows application, not service installation. Persistent
// installation/profile IDs are explicit; per-process instance IDs, paging keys
// and the installed API descriptor digest must be provided by composition.
type Config struct {
	NativeConsentPrefix string        `env:"NATIVE_CONSENT_PREFIX,required"`
	ApprovalBase        string        `env:"APPROVAL_BASE,required"`
	InstallationID      string        `env:"INSTALLATION_ID,required"`
	StateDirectory      string        `env:"STATE_DIRECTORY,required"`
	RuntimeInstanceFile string        `env:"RUNTIME_INSTANCE_FILE"`
	ServiceSID          string        `env:"SERVICE_SID,required"`
	AllowedSIDs         []string      `env:"ALLOWED_SIDS,required"`
	DefaultProfileID    string        `env:"DEFAULT_PROFILE_ID,required"`
	ControlOrigin       string        `env:"CONTROL_ORIGIN,required"`
	ProfileDisplayName  string        `env:"PROFILE_DISPLAY_NAME,required"`
	StartTimeout        time.Duration `env:"START_TIMEOUT,required"`
	StopTimeout         time.Duration `env:"STOP_TIMEOUT,required"`
	StatusTimeout       time.Duration `env:"STATUS_TIMEOUT,required"`
	CheckpointInterval  time.Duration `env:"CHECKPOINT_INTERVAL,required"`
	HandshakeTimeout    time.Duration `env:"HANDSHAKE_TIMEOUT,required"`
	DrainTimeout        time.Duration `env:"DRAIN_TIMEOUT,required"`
	StateLockRetry      time.Duration `env:"STATE_LOCK_RETRY,required"`
	CallTimeout         time.Duration `env:"CALL_TIMEOUT,required"`
	RetryInterval       time.Duration `env:"RETRY_INTERVAL,required"`
	WriteBudget         time.Duration `env:"WRITE_BUDGET,required"`
	MaxStatePlainBytes  int64         `env:"MAX_STATE_PLAIN_BYTES,required"`
	MaxStateCipherBytes int64         `env:"MAX_STATE_CIPHER_BYTES,required"`
	MaxMessageBytes     int           `env:"MAX_MESSAGE_BYTES,required"`
	MaxConnections      int           `env:"MAX_CONNECTIONS,required"`
	MaxConcurrentRPCs   uint32        `env:"MAX_CONCURRENT_RPCS,required"`
	MaxProfiles         int           `env:"MAX_PROFILES,required"`
	MaxDisplayNameBytes int           `env:"MAX_DISPLAY_NAME_BYTES,required"`
	MaxHostnameBytes    int           `env:"MAX_HOSTNAME_BYTES,required"`
	MaxPending          int           `env:"MAX_PENDING,required"`
	MaxCommitRetries    int           `env:"MAX_COMMIT_RETRIES,required"`
	MaxBootstrapBytes   int           `env:"MAX_BOOTSTRAP_BYTES,required"`
	MaxRequestBytes     int           `env:"MAX_REQUEST_BYTES,required"`
	MaxResponseBytes    int           `env:"MAX_RESPONSE_BYTES,required"`
	MaxCredentialBytes  int           `env:"MAX_CREDENTIAL_BYTES,required"`
	MaxQueryCalls       int           `env:"MAX_QUERY_CALLS,required"`
	MaxAccounts         int           `env:"MAX_ACCOUNTS,required"`
	MaxTargets          int           `env:"MAX_TARGETS,required"`
	UpstreamPageSize    uint32        `env:"UPSTREAM_PAGE_SIZE,required"`
	TelemetryConfig
	ConnectionConfig `envPrefix:"CONNECTION_"`
}

func ParseWindows(environment map[string]string) (Config, error) {
	if environment == nil {
		return Config{}, ErrConfig
	}
	var c Config
	// Never return parser diagnostics: invalid values may contain credentials.
	if err := env.ParseWithOptions(&c, env.Options{Environment: environment, Prefix: "ENDLESSNET_CLIENT_"}); err != nil {
		return Config{}, ErrConfig
	}
	if err := c.Validate(); err != nil {
		return Config{}, ErrConfig
	}
	return c, nil
}

var configID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validConfigID(value string) bool {
	return configID.MatchString(value) && value != "00000000-0000-0000-0000-000000000000"
}

func configSID(value string) bool {
	sid, err := windows.StringToSid(value)
	return err == nil && sid.String() == value
}

func stateDirectory(value string) bool {
	volume := filepath.VolumeName(value)
	if len(volume) != 2 {
		return false
	}
	drive := volume[0] | 0x20
	if volume[1] != ':' || drive < 'a' || drive > 'z' || !filepath.IsAbs(value) || filepath.Clean(value) != value || len(value) <= 3 {
		return false
	}
	for _, part := range strings.Split(value[3:], `\`) {
		if !filepath.IsLocal(part) || strings.ContainsAny(part, `<>:"|?*/`) || strings.TrimRight(part, " .") != part || strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return false
		}
	}
	return true
}

func controlOrigin(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Host != strings.ToLower(u.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(value, "?#\\") || u.String() != value || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return false
		}
	}
	return u.Hostname() != ""
}

func (c Config) Validate() error {
	if c.RuntimeInstanceFile != "" && (!stateDirectory(c.RuntimeInstanceFile) || c.RuntimeInstanceFile == c.StateDirectory) {
		return ErrConfig
	}
	if !approvalmodel.ValidBase(c.ApprovalBase) {
		return ErrConfig
	}
	if c.NativeConsentPrefix != "/api/v1/auth/native-login/" && c.NativeConsentPrefix != "/auth/native-login/" {
		return ErrConfig
	}
	if !validConfigID(c.InstallationID) || !validConfigID(c.DefaultProfileID) || !stateDirectory(c.StateDirectory) || !configSID(c.ServiceSID) || len(c.AllowedSIDs) == 0 || !controlOrigin(c.ControlOrigin) {
		return ErrConfig
	}
	seen := map[string]bool{}
	for _, sid := range c.AllowedSIDs {
		if !configSID(sid) || seen[sid] {
			return ErrConfig
		}
		seen[sid] = true
	}
	for _, n := range []int{c.MaxMessageBytes, c.MaxConnections, c.MaxProfiles, c.MaxDisplayNameBytes, c.MaxHostnameBytes, c.MaxPending, c.MaxCommitRetries, c.MaxBootstrapBytes, c.MaxRequestBytes, c.MaxResponseBytes, c.MaxCredentialBytes, c.MaxQueryCalls, c.MaxAccounts, c.MaxTargets} {
		if n <= 0 {
			return ErrConfig
		}
	}
	if c.MaxStatePlainBytes <= 0 || c.MaxStateCipherBytes <= c.MaxStatePlainBytes || c.MaxStateCipherBytes >= int64(math.MaxInt) || c.MaxBootstrapBytes == math.MaxInt || c.MaxPending < c.MaxProfiles || c.UpstreamPageSize == 0 || c.UpstreamPageSize > 500 || c.MaxConcurrentRPCs == 0 {
		return ErrConfig
	}
	if strings.TrimSpace(c.ProfileDisplayName) == "" || len(c.ProfileDisplayName) > c.MaxDisplayNameBytes || strings.IndexFunc(c.ProfileDisplayName, unicode.IsControl) >= 0 {
		return ErrConfig
	}
	maxPending := time.Duration(math.MaxUint32) * time.Millisecond
	for _, d := range []time.Duration{c.StartTimeout, c.StopTimeout} {
		if d < time.Millisecond || d > maxPending {
			return ErrConfig
		}
	}
	for _, d := range []time.Duration{c.StatusTimeout, c.CheckpointInterval} {
		if d < time.Millisecond || d > c.StartTimeout || d > c.StopTimeout {
			return ErrConfig
		}
	}
	for _, d := range []time.Duration{c.HandshakeTimeout, c.DrainTimeout, c.CallTimeout, c.RetryInterval, c.WriteBudget} {
		if d <= 0 || d > 30*time.Second {
			return ErrConfig
		}
	}
	if c.HandshakeTimeout > c.StartTimeout || c.DrainTimeout >= c.StopTimeout || c.WriteBudget >= c.StopTimeout || c.StateLockRetry <= 0 || c.StateLockRetry > time.Second {
		return ErrConfig
	}
	if c.TelemetryConfig.Validate() != nil || c.validateConnection() != nil {
		return ErrConfig
	}
	return nil
}

package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"strings"
)

const (
	AgentTLSModeStrict      = "strict"
	AgentTLSModeSystemStore = "system-store"
)

var (
	provider       Provider
	settings       = map[string]Setting{}
	InjectDefaults string

	SystemAgentInstallScript = NewSetting("system-agent-install-script", "https://github.com/rancher/system-agent/releases/download/v0.3.10-rc.1/install.sh")
	WinsAgentInstallScript   = NewSetting("wins-agent-install-script", "")
	SystemAgentVersion       = NewSetting("system-agent-version", "v0.3.10")
	WinsAgentVersion         = NewSetting("wins-agent-version", "")
	ServerURL                = NewSetting("server-url", "")
	CAPIAPIServerURL         = NewSetting("capi-api-server-url", "") // kind server LB
	UIPath                   = NewSetting("ui-path", "/usr/share/rancher/ui")
	AgentTLSMode             = NewSetting("agent-tls-mode", AgentTLSModeSystemStore)
	CSIProxyAgentVersion     = NewSetting("csi-proxy-agent-version", "")
	CSIProxyAgentURL         = NewSetting("csi-proxy-agent-url", "https://acs-mirror.azureedge.net/csi-proxy/%[1]s/binaries/csi-proxy-%[1]s.tar.gz")
	CACerts                  = NewSetting("cacerts", "")
	InternalCACerts          = NewSetting("internal-cacerts", "")
	SystemDefaultRegistry    = NewSetting("system-default-registry", "")
)

func init() {
	if InjectDefaults == "" {
		return
	}
	defaults := map[string]string{}
	if err := json.Unmarshal([]byte(InjectDefaults), &defaults); err != nil {
		return
	}
	for name, defaultValue := range defaults {
		value, ok := settings[name]
		if !ok {
			continue
		}
		value.Default = defaultValue
		settings[name] = value
	}
}

func InternalCAChecksum() string {
	ca := InternalCACerts.Get()
	if ca != "" {
		if !strings.HasSuffix(ca, "\n") {
			ca += "\n"
		}
		digest := sha256.Sum256([]byte(ca))
		return hex.EncodeToString(digest[:])
	}
	return ""
}

func CAChecksum() string {
	ca := CACerts.Get()
	if ca != "" {
		if !strings.HasSuffix(ca, "\n") {
			ca += "\n"
		}
		digest := sha256.Sum256([]byte(ca))
		return hex.EncodeToString(digest[:])
	}
	return ""
}

type Setting struct {
	Name    string
	Default string
}

// Get will return the currently stored value of the setting.
func (s Setting) Get() string {
	if provider == nil {
		s := settings[s.Name]
		return s.Default
	}
	return provider.Get(s.Name)
}

func (s Setting) Set(value string) {
	if provider == nil {
		s.Default = value
		return
	}
	provider.Set(s.Name, value)
}

type Provider interface {
	Get(name string) string
	Set(name, value string) error
	SetIfUnset(name, value string) error
	SetAll(settings map[string]Setting) error
}

func NewSetting(name string, d string) Setting {
	logrus.Debugf("Setting new setting (%s) with default (%s)", name, d)
	settings[name] = Setting{
		Name:    name,
		Default: d,
	}
	return settings[name]
}

// SetProvider will set the given provider as the global provider for all settings.
func SetProvider(p Provider) error {
	if err := p.SetAll(settings); err != nil {
		return err
	}
	provider = p
	return nil
}

// GetEnvKey will return the given string formatted as a CAPR environmental variable.
func GetEnvKey(key string) string {
	return "CAPR_" + strings.ToUpper(strings.Replace(key, "-", "_", -1))
}

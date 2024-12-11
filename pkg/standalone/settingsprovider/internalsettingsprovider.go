package settingsprovider

import (
	caprcontext "github.com/rancher/cluster-api-provider-rancher/pkg/context"
	"github.com/rancher/cluster-api-provider-rancher/pkg/settings"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"os"
)

const (
	TlsCAName      = "tls-capr-ca"
	TlsCANamespace = "kube-system"
)

func (sp *settingsProvider) OnCAChange(key string, secret *corev1.Secret) (*corev1.Secret, error) {
	if secret == nil || secret.Name != TlsCAName || secret.Namespace != TlsCANamespace {
		return secret, nil
	}
	logrus.Debugf("[internalsettingsprovider] reconciling CA secret %s/%s", secret.Namespace, secret.Name)
	sp.store["cacerts"] = string(secret.Data["tls.crt"])
	return secret, nil
}

func Register(wContext *caprcontext.Context) error {
	sp := &settingsProvider{
		store:    map[string]string{},
		defaults: map[string]string{},
	}

	err := settings.SetProvider(sp)
	if err != nil {
		return err
	}
	wContext.Core.Secret().OnChange(wContext.Ctx, "standalone-certificate-authority", sp.OnCAChange)
	return nil
}

type settingsProvider struct {
	store    map[string]string
	defaults map[string]string
}

func (s *settingsProvider) Get(name string) string {
	value := os.Getenv(settings.GetEnvKey(name))
	if value != "" {
		return value
	}

	if v, ok := s.store[name]; ok {
		return v
	}
	return s.defaults[name]
}

func (s *settingsProvider) Set(name, value string) error {
	logrus.Tracef("Setting (%s) is being set to value: %s", name, value)
	s.store[name] = value
	return nil
}

func (s *settingsProvider) SetIfUnset(name, value string) error {
	if _, ok := s.store[name]; !ok {
		s.store[name] = value
	}
	return nil
}

// SetAll iterates through a map of settings.Setting and updates corresponding settings in k8s
// to match any values set for them via their respective CATTLE_<setting-name> env var, their
// source to "env" if configured by an env var, and their default to match the setting in the map.
// NOTE: All settings not provided in settingsMap will be marked as unknown, and may be removed in the future.
func (s *settingsProvider) SetAll(settingsMap map[string]settings.Setting) error {
	for name, setting := range settingsMap {
		key := settings.GetEnvKey(name)
		envValue, envOk := os.LookupEnv(key)
		s.defaults[name] = setting.Default
		s.store[name] = setting.Default
		if envOk {
			s.store[name] = envValue
		}
	}
	return nil
}

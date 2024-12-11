package configserver

import (
	"crypto/sha256"
	"encoding/base64"
	caprcommon "github.com/rancher/cluster-api-provider-rancher/pkg"
	caprcontext "github.com/rancher/cluster-api-provider-rancher/pkg/context"
	rkecontrollers "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/rke.cattle.io/v1"
	"github.com/rancher/cluster-api-provider-rancher/pkg/settings"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"net/http"
	"strings"
)

var tokenIndex string = "tokenIndex"

type StandaloneConfigServerResolver struct {
	secretCache         corecontrollers.SecretCache
	serviceAccountCache corecontrollers.ServiceAccountCache
	rkeBootstrapCache   rkecontrollers.RKEBootstrapCache
	informers           map[string]cache.SharedIndexInformer
}

func NewStandaloneConfigServerResolver(wContext *caprcontext.Context) StandaloneConfigServerResolver {
	wContext.Core.Secret().Cache().AddIndexer(tokenIndex, func(obj *corev1.Secret) ([]string, error) {
		if obj.Type == corev1.SecretTypeServiceAccountToken {
			hash := sha256.Sum256(obj.Data["token"])
			return []string{base64.URLEncoding.EncodeToString(hash[:])}, nil
		}
		return nil, nil
	})
	return StandaloneConfigServerResolver{
		secretCache:         wContext.Core.Secret().Cache(),
		serviceAccountCache: wContext.Core.ServiceAccount().Cache(),
		rkeBootstrapCache:   wContext.RKE.RKEBootstrap().Cache(),
		informers: map[string]cache.SharedIndexInformer{
			"Secret":         wContext.Core.Secret().Informer(),
			"ServiceAccount": wContext.Core.ServiceAccount().Informer(),
			"RKEBootstrap":   wContext.RKE.RKEBootstrap().Informer(),
		},
	}
}

func (s StandaloneConfigServerResolver) Ready() (bool, error) {
	var informerNotReady bool
	for informerName, informer := range s.informers {
		if !informer.HasSynced() {
			informerNotReady = true
			if err := informer.GetIndexer().Resync(); err != nil {
				logrus.Errorf("error re-syncing %s informer in rke2configserver: %v", informerName, err)
			}
		}
	}
	return !informerNotReady, nil
}

func (s StandaloneConfigServerResolver) GetCorrespondingMachineByRequest(req *http.Request) (string, string, error) {
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	secrets, err := s.secretCache.GetByIndex(tokenIndex, token)
	if err != nil || len(secrets) == 0 {
		return "", "", err
	}

	sa, err := s.serviceAccountCache.Get(secrets[0].Namespace, secrets[0].Annotations[corev1.ServiceAccountNameKey])
	if err != nil {
		return "", "", err
	}

	if sa.Labels[caprcommon.RoleLabel] != caprcommon.RoleBootstrap || string(sa.UID) != secrets[0].Annotations[corev1.ServiceAccountUIDKey] {
		return "", "", err
	}

	if foundParent, err := caprcommon.IsOwnedByMachine(s.rkeBootstrapCache, sa.Labels[caprcommon.MachineNameLabel], sa); err != nil || !foundParent {
		return "", "", err
	}

	return sa.Namespace, sa.Labels[caprcommon.MachineNameLabel], nil
}

func (s StandaloneConfigServerResolver) GetK8sAPIServerURLAndCertificateByRequest(req *http.Request) (string, []byte) {
	ca := settings.CACerts.Get()

	var bytes []byte
	if strings.TrimSpace(ca) != "" {
		if !strings.HasSuffix(ca, "\n") {
			ca += "\n"
		}
		bytes = []byte(ca)
	}

	return settings.CAPIAPIServerURL.Get(), bytes
}

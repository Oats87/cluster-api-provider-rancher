package kubeconfig

import (
	"fmt"
	"github.com/rancher/cluster-api-provider-rancher/pkg"
	caprcontext "github.com/rancher/cluster-api-provider-rancher/pkg/context"
	capicontrollers "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta1"
	rkev1controllers "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/rke.cattle.io/v1"
	"github.com/rancher/cluster-api-provider-rancher/pkg/planner"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"reflect"
	"regexp"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

// from: snapshotbackplopulate controller in rancher
const (
	StorageAnnotationKey              = "etcdsnapshot.rke.io/storage"
	SnapshotNameKey                   = "etcdsnapshot.rke.io/snapshot-file-name"
	SnapshotBackpopulateReconciledKey = "etcdsnapshot.rke.io/snapshotbackpopulate-reconciled"
	StorageS3                         = "s3"
	StorageLocal                      = "local"
)

var (
	configMapNames = map[string]bool{
		"k3s-etcd-snapshots":  true,
		"rke2-etcd-snapshots": true,
	}
	InvalidKeyChars = regexp.MustCompile(`[^-.a-zA-Z0-9]`)
)

type handler struct {
	secrets             corecontrollers.SecretClient
	machinesCache       capicontrollers.MachineCache
	machinesClient      capicontrollers.MachineClient
	clusterCache        capicontrollers.ClusterCache
	etcdSnapshotsClient rkev1controllers.ETCDSnapshotClient
	etcdSnapshotsCache  rkev1controllers.ETCDSnapshotCache
}

func Register(wContext *caprcontext.Context) {
	h := handler{
		secrets:             wContext.Core.Secret(),
		machinesCache:       wContext.CAPI.Machine().Cache(),
		machinesClient:      wContext.CAPI.Machine(),
		clusterCache:        wContext.CAPI.Cluster().Cache(),
		etcdSnapshotsClient: wContext.RKE.ETCDSnapshot(),
		etcdSnapshotsCache:  wContext.RKE.ETCDSnapshot().Cache(),
	}
	wContext.Core.Secret().OnChange(wContext.Ctx, "plan-secret", h.OnChange)
}

func (h *handler) OnChange(key string, secret *corev1.Secret) (*corev1.Secret, error) {
	if secret == nil || secret.Type != capr.SecretTypeMachinePlan || len(secret.Data) == 0 || secret.Labels[capr.InitNodeLabel] != "true" {
		return secret, nil
	}
	logrus.Debugf("[standalone kubeconfig] reconciling plan secret %s/%s to capture kubeconfig", secret.Namespace, secret.Name)

	node, err := planner.SecretToNode(secret)
	if err != nil {
		return secret, err
	}

	if v, ok := node.PeriodicOutput["dump-kubeconfig"]; ok && v.ExitCode == 0 && len(v.Stdout) > 0 {
		if err := h.reconcileKubeconfig(secret.Namespace, secret.Labels[capr.ClusterNameLabel], v.Stdout); err != nil {
			logrus.Errorf("[plansecret] error reconciling kubeconfig for secret %s/%s: %v", secret.Namespace, secret.Name, err)
		}
	}

	return secret, err
}

func (h *handler) reconcileKubeconfig(clusterNamespace, clusterName string, stdout []byte) error {
	cluster, err := h.clusterCache.Get(clusterNamespace, clusterName)
	if err != nil {
		return err
	}

	config, err := clientcmd.Load(stdout)
	if err != nil {
		return err
	}

	for k := range config.Clusters {
		// TODO: fix this stupid workaround - but we need to do it because the kube-apiserver doesn't have the tls-san for the infracluster endpoint
		config.Clusters[k].CertificateAuthorityData = nil
		config.Clusters[k].InsecureSkipTLSVerify = true
		config.Clusters[k].Server = fmt.Sprintf("https://%s:%d", cluster.Spec.ControlPlaneEndpoint.Host, cluster.Spec.ControlPlaneEndpoint.Port)
	}

	kc, err := clientcmd.Write(*config)
	if err != nil {
		return err
	}

	existingKCSecret, err := h.secrets.Get(clusterNamespace, fmt.Sprintf("%s-kubeconfig", clusterName), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = h.secrets.Create(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-kubeconfig", clusterName),
				Namespace: clusterNamespace,
				Labels: map[string]string{
					capi.ClusterNameLabel: clusterName,
				},
			},
			Data: map[string][]byte{
				"value": kc,
			},
		})
		return err
	} else if err != nil {
		return err
	}

	if !reflect.DeepEqual(existingKCSecret.Data["value"], []byte(kc)) {
		existingKCSecret.Data["value"] = []byte(kc)
		_, err = h.secrets.Update(existingKCSecret)
	}

	return err
}

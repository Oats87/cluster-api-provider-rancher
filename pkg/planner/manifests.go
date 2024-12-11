package planner

import (
	"encoding/base64"
	"fmt"
	"path"

	"github.com/rancher/cluster-api-provider-rancher/pkg"
	rkev1 "github.com/rancher/cluster-api-provider-rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/cluster-api-provider-rancher/pkg/apis/rke.cattle.io/v1/plan"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const EtcdSnapshotExtraMetadataConfigMapTemplate = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: %s-etcd-snapshot-extra-metadata
  namespace: %s
data:
  %s: %s
`

// getControlPlaneManifests returns a slice of plan.File objects that are necessary to be placed on a controlplane node.
func (p *Planner) getControlPlaneManifests(controlPlane *rkev1.RKEControlPlane, entry *planEntry) (result []plan.File, _ error) {
	// NOTE: The agent does not have a means to delete files.  If you add a manifest that
	// may not exist in the future then you should create an empty file to "delete" the file
	if !isControlPlane(entry) {
		return nil, nil
	}

	taints, err := getTaints(entry, controlPlane)
	if err != nil {
		return nil, err
	}

	controlPlaneManifests, err := p.retrievalFunctions.ControlPlaneManifests(controlPlane, taints)
	if err != nil {
		return nil, err
	}
	result = append(result, controlPlaneManifests...)

	// if we have a nil snapshotMetadata object, it's probably because the annotation didn't exist on the controlplane object. this is not breaking though so don't block.
	snapshotMetadata := getEtcdSnapshotExtraMetadata(controlPlane, capr.GetRuntime(controlPlane.Spec.KubernetesVersion))
	if snapshotMetadata == nil {
		logrus.Errorf("Error while generating etcd snapshot extra metadata manifest for cluster %s", controlPlane.Spec.ClusterName)
	} else {
		result = append(result, *snapshotMetadata)
	}

	addons := p.getAddons(controlPlane)
	result = append(result, addons)

	return result, nil
}

// getEtcdSnapshotExtraMetadata returns a plan.File that contains the ConfigMap manifest of the cluster specification, if it exists.
// Otherwise, it will return an empty plan.File and log an error.
func getEtcdSnapshotExtraMetadata(controlPlane *rkev1.RKEControlPlane, runtime string) *plan.File {
	if v, ok := controlPlane.Annotations[capr.ClusterSpecAnnotation]; ok {
		cm := fmt.Sprintf(EtcdSnapshotExtraMetadataConfigMapTemplate, runtime, metav1.NamespaceSystem, EtcdSnapshotConfigMapKey, v)
		return &plan.File{
			Content: base64.StdEncoding.EncodeToString([]byte(cm)),
			Path:    path.Join(capr.GetDistroDataDir(controlPlane), fmt.Sprintf("server/manifests/rancher/%s-etcd-snapshot-extra-metadata.yaml", runtime)),
			Dynamic: true,
			Minor:   true,
		}
	}

	logrus.Errorf("rkecluster %s/%s: unable to find cluster spec annotation for control plane", controlPlane.Spec.ClusterName, controlPlane.Namespace)
	return nil
}

// getAddons returns a plan.File that contains the content of the defined additional manifests.
func (p *Planner) getAddons(controlPlane *rkev1.RKEControlPlane) plan.File {
	return plan.File{
		Content: base64.StdEncoding.EncodeToString([]byte(controlPlane.Spec.AdditionalManifest)),
		Path:    path.Join(capr.GetDistroDataDir(controlPlane), "server/manifests/rancher/addons.yaml"),
		Dynamic: true,
	}
}
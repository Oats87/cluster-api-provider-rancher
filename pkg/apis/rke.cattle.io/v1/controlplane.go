package v1

import (
	"github.com/rancher/wrangler/v3/pkg/genericcondition"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"time"
)

const (
	// RKEControlPlaneLegacyFinalizer allows the controller to clean up resources on delete.
	// this is the old finalizer name. It is kept to ensure backward compatibility.
	RKEControlPlaneLegacyFinalizer = "rke.controleplane.cluster.x-k8s.io"
	// RKEControlPlaneFinalizer allows the controller to clean up resources on delete.
	RKEControlPlaneFinalizer = "rke.controlplane.cluster.x-k8s.io"

	// LegacyRKE2ControlPlane is a controlplane annotation that marks the CP as legacy. This CP will not provide
	// etcd certificate management or etcd membership management.
	LegacyRKEControlPlane = "controlplane.cluster.x-k8s.io/legacy"
)

// +genclient
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels=cluster.x-k8s.io/v1beta1=v1
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type RKEControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RKEControlPlaneSpec   `json:"spec"`
	Status            RKEControlPlaneStatus `json:"status,omitempty"`
}

type EnvVar struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// RKEControlPlaneMachineTemplate defines the template for Machines
// in a RKEControlPlane object.
type RKEControlPlaneMachineTemplate struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta capi.ObjectMeta `json:"metadata,omitempty"`

	// InfrastructureRef is a required reference to a custom resource
	// offered by an infrastructure provider.
	InfrastructureRef corev1.ObjectReference `json:"infrastructureRef"`

	// NodeDrainTimeout is the total amount of time that the controller will spend on draining a controlplane node
	// The default value is 0, meaning that the node can be drained without any time limitations.
	// NOTE: NodeDrainTimeout is different from `kubectl drain --timeout`
	// +optional
	NodeDrainTimeout *metav1.Duration `json:"nodeDrainTimeout,omitempty"`
}

// RolloutStrategy describes how to replace existing machines
// with new ones.
type RolloutStrategy struct {
	// Type of rollout. Currently the only supported strategy is "RollingUpdate".
	// Default is RollingUpdate.
	// +optional
	Type RolloutStrategyType `json:"type,omitempty"`

	// Rolling update config params. Present only if RolloutStrategyType = RollingUpdate.
	// +optional
	RollingUpdate *RollingUpdate `json:"rollingUpdate,omitempty"`
}

// RollingUpdate is used to control the desired behavior of rolling update.
type RollingUpdate struct {
	// The maximum number of control planes that can be scheduled above or under the
	// desired number of control planes.
	// Value can be an absolute number 1 or 0.
	// Defaults to 1.
	// Example: when this is set to 1, the control plane can be scaled
	// up immediately when the rolling update starts.
	// +optional
	MaxSurge *intstr.IntOrString `json:"maxSurge,omitempty"`
}

// RolloutStrategyType defines the rollout strategies for a RKE2ControlPlane.
type RolloutStrategyType string

const (
	// RollingUpdateStrategyType replaces the old control planes by new one using rolling update
	// i.e. gradually scale up or down the old control planes and scale up or down the new one.
	RollingUpdateStrategyType RolloutStrategyType = "RollingUpdate"

	// PreTerminateHookCleanupAnnotation is the annotation RKE2 sets on Machines to ensure it can later remove the
	// etcd member right before Machine termination (i.e. before InfraMachine deletion).
	// For RKE2 we need wait for all other pre-terminate hooks to finish to
	// ensure it runs last (thus ensuring that kubelet is still working while other pre-terminate hooks run
	// as it uses kubelet local mode).
	PreTerminateHookCleanupAnnotation = capi.PreTerminateDeleteHookAnnotationPrefix + "/rke2-cleanup"
)

type RKEControlPlaneSpec struct {
	RKEClusterSpecCommon `json:",inline"`

	// +optional
	AgentEnvVars             []EnvVar                 `json:"agentEnvVars,omitempty"`
	LocalClusterAuthEndpoint LocalClusterAuthEndpoint `json:"localClusterAuthEndpoint"`
	// +optional
	ETCDSnapshotCreate *ETCDSnapshotCreate `json:"etcdSnapshotCreate,omitempty"`
	// +optional
	ETCDSnapshotRestore *ETCDSnapshotRestore `json:"etcdSnapshotRestore,omitempty"`
	// +optional
	RotateCertificates *RotateCertificates `json:"rotateCertificates,omitempty"`
	// +optional
	RotateEncryptionKeys *RotateEncryptionKeys `json:"rotateEncryptionKeys,omitempty"`
	// +optional
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
	// +optional
	ClusterName string `json:"clusterName,omitempty" wrangler:"required"`
	// +optional
	ManagementClusterName string `json:"managementClusterName,omitempty" wrangler:"required"`
	// +optional
	UnmanagedConfig bool `json:"unmanagedConfig,omitempty"`

	// Replicas is the number of replicas for the Control Plane.
	Replicas *int32 `json:"replicas,omitempty"`

	// MachineTemplate contains information about how machines
	// should be shaped when creating or updating a control plane.
	// +optional
	MachineTemplate RKEControlPlaneMachineTemplate `json:"machineTemplate,omitempty"`

	// +kubebuilder:validation:Pattern="(v\\d\\.\\d{2}\\.\\d+\\+rke2r\\d)|^$"
	// +optional
	Version string `json:"version"`

	// InfrastructureRef is a required reference to a custom resource
	// offered by an infrastructure provider.
	// +optional
	InfrastructureRef corev1.ObjectReference `json:"infrastructureRef"`

	// The RolloutStrategy to use to replace control plane machines with new ones.
	// +optional
	RolloutStrategy *RolloutStrategy `json:"rolloutStrategy"`
}

type RKEControlPlaneStatus struct {
	// +optional
	AppliedSpec *RKEControlPlaneSpec `json:"appliedSpec,omitempty"`
	// +optional
	Conditions []genericcondition.GenericCondition `json:"conditions,omitempty"`
	// +optional
	Ready bool `json:"ready,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration"`
	// +optional
	CertificateRotationGeneration int64 `json:"certificateRotationGeneration"`
	// +optional
	RotateEncryptionKeys *RotateEncryptionKeys `json:"rotateEncryptionKeys,omitempty"`
	// +optional
	RotateEncryptionKeysPhase RotateEncryptionKeysPhase `json:"rotateEncryptionKeysPhase,omitempty"`
	// +optional
	RotateEncryptionKeysLeader string `json:"rotateEncryptionKeysLeader,omitempty"`
	// +optional
	ETCDSnapshotRestore *ETCDSnapshotRestore `json:"etcdSnapshotRestore,omitempty"`
	// +optional
	ETCDSnapshotRestorePhase ETCDSnapshotPhase `json:"etcdSnapshotRestorePhase,omitempty"`
	// +optional
	ETCDSnapshotCreate *ETCDSnapshotCreate `json:"etcdSnapshotCreate,omitempty"`
	// +optional
	ETCDSnapshotCreatePhase ETCDSnapshotPhase `json:"etcdSnapshotCreatePhase,omitempty"`
	// +optional
	ConfigGeneration int64 `json:"configGeneration,omitempty"`
	// +optional
	Initialized bool `json:"initialized,omitempty"`
	// +optional
	AgentConnected bool `json:"agentConnected,omitempty"`

	// Replicas is the number of replicas current attached to this ControlPlane Resource.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Version represents the minimum Kubernetes version for the control plane machines
	// in the cluster.
	// +optional
	Version *string `json:"version,omitempty"`

	// ReadyReplicas is the number of replicas current attached to this ControlPlane Resource and that have Ready Status.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// UpdatedReplicas is the number of replicas current attached to this ControlPlane Resource and that are up-to-date with Control Plane config.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`

	// UnavailableReplicas is the number of replicas current attached to this ControlPlane Resource and that are up-to-date with Control Plane config.
	// +optional
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty"`
}

// GetConditions returns the list of conditions for a RKE2ControlPlane object.
func (r *RKEControlPlane) GetConditions() capi.Conditions {
	conditions := capi.Conditions{}
	for _, c := range r.Status.Conditions {
		condition := capi.Condition{
			Type:   capi.ConditionType(c.Type),
			Status: c.Status,
			// Severity: <no severity...>,
			Reason:  c.Reason,
			Message: c.Message,
		}
		if c.LastTransitionTime != "" {
			t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", c.LastTransitionTime)
			if err != nil {
				continue
			}
			condition.LastTransitionTime = metav1.NewTime(t)
		}
		conditions = append(conditions, condition)
	}
	return conditions
}

// SetConditions sets the list of conditions for a RKE2ControlPlane object.
func (r *RKEControlPlane) SetConditions(conditions capi.Conditions) {
	conditionsToSet := []genericcondition.GenericCondition{}
	for _, c := range conditions {
		condition := genericcondition.GenericCondition{
			Type:               string(c.Type),
			Status:             c.Status,
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime.String(),
		}
		conditionsToSet = append(conditionsToSet, condition)
	}
	r.Status.Conditions = conditionsToSet
}

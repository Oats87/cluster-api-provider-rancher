package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels=cluster.x-k8s.io/v1beta1=v1
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type RKEBootstrap struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RKEBootstrapSpec   `json:"spec"`
	Status            RKEBootstrapStatus `json:"status,omitempty"`
}

type RKEBootstrapSpec struct {
	ClusterName string `json:"clusterName,omitempty"`
	// +kubebuilder:validation:Pattern="(v\\d\\.\\d{2}\\.\\d+\\+rke2r\\d)|^$"
	// +optional
	Version string `json:"version"`
}

type RKEBootstrapStatus struct {
	// Ready indicates the BootstrapData field is ready to be consumed
	Ready bool `json:"ready,omitempty"`

	// DataSecretName is the name of the secret that stores the bootstrap data script.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`
}

// +genclient
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels=cluster.x-k8s.io/v1beta1=v1
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type RKEBootstrapTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RKEBootstrapTemplateSpec `json:"spec" wrangler:"required"`
}

type RKEBootstrapTemplateSpec struct {
	ClusterName string       `json:"clusterName,omitempty"`
	Template    RKEBootstrap `json:"template,omitempty" wrangler:"required"`
}

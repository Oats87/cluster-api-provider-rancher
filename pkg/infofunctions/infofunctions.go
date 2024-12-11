package infofunctions

import (
	"context"
	"github.com/rancher/channelserver/pkg/model"
	rkev1 "github.com/rancher/cluster-api-provider-rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/cluster-api-provider-rancher/pkg/apis/rke.cattle.io/v1/plan"
	corev1 "k8s.io/api/core/v1"
)

type ImageResolver func(image string, cp *rkev1.RKEControlPlane) string
type ReleaseData func(context.Context, *rkev1.RKEControlPlane) *model.Release
type SystemAgentImage func() string
type SystemPodLabelSelectors func(plane *rkev1.RKEControlPlane) []string
type ControlPlaneManifests func(plane *rkev1.RKEControlPlane, taints []corev1.Taint) ([]plan.File, error)

type GetToken func(plane *rkev1.RKEControlPlane) string

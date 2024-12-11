package rkecontrolplane

import (
	"context"
	"fmt"
	capr "github.com/rancher/cluster-api-provider-rancher/pkg"
	rkev1 "github.com/rancher/cluster-api-provider-rancher/pkg/apis/rke.cattle.io/v1"
	"github.com/rancher/cluster-api-provider-rancher/pkg/apis/rke.cattle.io/v1/plan"
	caprcontext "github.com/rancher/cluster-api-provider-rancher/pkg/context"
	caprplannercontroller "github.com/rancher/cluster-api-provider-rancher/pkg/controllers/planner"
	capicontrollers "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta1"
	rkecontroller "github.com/rancher/cluster-api-provider-rancher/pkg/generated/controllers/rke.cattle.io/v1"
	caprplanner "github.com/rancher/cluster-api-provider-rancher/pkg/planner"
	"github.com/rancher/lasso/pkg/dynamic"
	"github.com/rancher/wrangler/v3/pkg/condition"
	"github.com/rancher/wrangler/v3/pkg/data"
	"github.com/rancher/wrangler/v3/pkg/name"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
	"strconv"
	"strings"
	"time"
)

func Register(c *caprcontext.Context) {
	registerStandalone(c)
	registerCommon(c)
}

func registerCommon(c *caprcontext.Context) {
	p := caprplanner.New(c, caprplanner.InfoFunctions{
		ImageResolver:           func(image string, cp *rkev1.RKEControlPlane) string { return image },
		ReleaseData:             capr.GetKDMReleaseData,
		SystemAgentImage:        func() string { return "rancher/system-agent-installer-" },
		SystemPodLabelSelectors: func(plane *rkev1.RKEControlPlane) []string { return []string{} },
		ControlPlaneManifests: func(plane *rkev1.RKEControlPlane, taints []corev1.Taint) ([]plan.File, error) {
			return []plan.File{}, nil
		},
	})

	caprplannercontroller.Register(c, p)
}

func registerStandalone(c *caprcontext.Context) {
	h := handler{
		ctx:            c.Ctx,
		machineClient:  c.CAPI.Machine(),
		bootstrapCache: c.RKE.RKEBootstrap().Cache(),
		dynamic:        c.Dynamic,
	}

	rkecontroller.RegisterRKEControlPlaneGeneratingHandler(c.Ctx,
		c.RKE.RKEControlPlane(),
		c.Apply.
			// Because capi wants to own objects we don't set ownerreference with apply
			WithDynamicLookup().
			WithCacheTypes(
				c.CAPI.Machine(),
				c.RKE.RKEBootstrap(),
			),
		"",
		"rke-control-plane-standalone",
		h.GenerateMachinesAndRKEBootstrap,
		nil)
}

type handler struct {
	ctx            context.Context
	machineClient  capicontrollers.MachineClient
	bootstrapCache rkecontroller.RKEBootstrapCache
	dynamic        *dynamic.Controller
}

func (h *handler) GenerateMachinesAndRKEBootstrap(controlplane *rkev1.RKEControlPlane, status rkev1.RKEControlPlaneStatus) ([]runtime.Object, rkev1.RKEControlPlaneStatus, error) {
	logrus.Infof("[rkecontrolplane standalone] Generating machines and RKE Bootstrap for cluster %s/%s", controlplane.Namespace, controlplane.Name)

	if controlplane.DeletionTimestamp != nil || (controlplane.Spec.Replicas == &[]int32{0}[0]) {
		logrus.Infof("[rkecontrolplane standalone] RKEControlPlane %s/%s desired replica count is 0", controlplane.Namespace, controlplane.Name)

		return nil, status, nil
	}

	var objects []runtime.Object
	machines, err := h.machineClient.List(controlplane.Namespace, metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s,%s=%s", capi.ClusterNameLabel, controlplane.Name, "managed-by", "rkecontrolplane")})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, status, err
	}
	currentReplicaCount := int32(len(machines.Items))

	for _, existingMachine := range machines.Items {
		logrus.Infof("[rkecontrolplane standalone] Generating appliable machine/bootstrap/infraobject (%s/%s/%s) from existing machines", existingMachine.Name, existingMachine.Spec.Bootstrap.ConfigRef.Name, existingMachine.Spec.InfrastructureRef.Name)
		io, err := h.createInfraObjectFromTemplate(controlplane, existingMachine.Name)
		if err != nil {
			return nil, status, err
		}
		machine, bootstrap := generateMachineAndRKEBootstrap(controlplane, existingMachine.Name, existingMachine.Spec.Bootstrap.ConfigRef.Name, corev1.ObjectReference{
			APIVersion: io.GetAPIVersion(),
			Kind:       io.GetKind(),
			Name:       io.GetName(),
			Namespace:  io.GetNamespace(),
		})
		objects = append(objects, io)
		objects = append(objects, machine)
		objects = append(objects, bootstrap)
	}
	// TODO: add filter to determine necessary scale based on rollout. for MVP we don't need this, we can just match the replica count.
	for i := int32(0); i < (*controlplane.Spec.Replicas - currentReplicaCount); i++ {
		nameSuffix := strconv.FormatInt(time.Now().Unix(), 10)
		machineName := name.SafeConcatName(controlplane.Name, "machine", nameSuffix)
		bootstrapName := name.SafeConcatName(controlplane.Name, "bootstrap", nameSuffix)
		io, err := h.createInfraObjectFromTemplate(controlplane, machineName)
		if err != nil {
			return nil, status, err
		}
		logrus.Infof("[rkecontrolplane standalone] Generating new machine: %s and bootstrap: %s with infra object: (%s) %s/%s", machineName, bootstrapName, io.GetKind(), io.GetNamespace(), io.GetName())
		machine, bootstrap := generateMachineAndRKEBootstrap(controlplane, machineName, bootstrapName, corev1.ObjectReference{
			APIVersion: io.GetAPIVersion(),
			Kind:       io.GetKind(),
			Name:       io.GetName(),
			Namespace:  io.GetNamespace(),
		})
		objects = append(objects, io)
		objects = append(objects, machine)
		objects = append(objects, bootstrap)
	}

	if status.Replicas != *controlplane.Spec.Replicas {
		logrus.Tracef("[rkecontrolplane standalone] Updating replica count on status of RKEControlPlane %s/%s to %d", controlplane.Namespace, controlplane.Name, controlplane.Spec.Replicas)
		status.Replicas = *controlplane.Spec.Replicas
	}

	status.AgentConnected = true // there is no agent with standalone
	aCon := condition.Cond("Available")
	aCon.True(&status)
	status.Version = &controlplane.Spec.Version
	return objects, status, err
}

func (h *handler) createInfraObjectFromTemplate(controlplane *rkev1.RKEControlPlane, machineName string) (*unstructured.Unstructured, error) {
	infraTemplateRef := controlplane.Spec.InfrastructureRef
	infraTemplateApiVersion := infraTemplateRef.APIVersion
	infraTemplateKind := infraTemplateRef.Kind
	infraTemplateName := infraTemplateRef.Name
	infraTemplateNamespace := infraTemplateRef.Namespace

	// return error if apiversion and kind are empty

	gvk := schema.FromAPIVersionAndKind(infraTemplateApiVersion, infraTemplateKind)
	logrus.Infof("[rkecontrolplane standalone] GVK for the infrastructuremachinetemplate %s/%s was: %s", infraTemplateNamespace, infraTemplateName, gvk.String())
	infraTemplate, err := h.dynamic.Get(gvk, infraTemplateNamespace, infraTemplateName)
	if err != nil {
		return nil, err
	}

	infraTemplateData, err := data.Convert(infraTemplate.DeepCopyObject())
	if err != nil {
		return nil, err
	}

	labels, _, _ := unstructured.NestedMap(infraTemplateData, "metadata", "labels")
	annotations, _, _ := unstructured.NestedMap(infraTemplateData, "metadata", "annotations")
	spec, _, _ := unstructured.NestedMap(infraTemplateData, "spec", "template", "spec")

	ustr := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       strings.TrimSuffix(infraTemplateKind, "Template"),
			"apiVersion": infraTemplateApiVersion,
			"metadata": map[string]interface{}{
				"annotations": annotations,
				"name":        name.SafeConcatName(infraTemplateName, machineName),
				"namespace":   controlplane.Namespace,
				"labels":      labels,
			},
			"spec": spec,
		},
	}

	return ustr, nil
}

func generateMachineAndRKEBootstrap(controlplane *rkev1.RKEControlPlane, machineName, bootstrapName string, infraRef corev1.ObjectReference) (*capi.Machine, *rkev1.RKEBootstrap) {
	return &capi.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: controlplane.Namespace,
				Name:      machineName,
				Labels: map[string]string{
					capi.ClusterNameLabel:         controlplane.Name,
					capi.MachineControlPlaneLabel: "true",
					"managed-by":                  "rkecontrolplane",
				},
			},
			Spec: capi.MachineSpec{
				ClusterName: controlplane.Name,
				Bootstrap: capi.Bootstrap{
					ConfigRef: &corev1.ObjectReference{
						Kind:       "RKEBootstrap",
						Namespace:  controlplane.Namespace,
						Name:       bootstrapName,
						APIVersion: rkev1.SchemeGroupVersion.String(),
					},
				},
				InfrastructureRef: infraRef,
				Version:           &controlplane.Spec.KubernetesVersion,
			},
		},
		&rkev1.RKEBootstrap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: controlplane.Namespace,
				Name:      bootstrapName,
				Labels: map[string]string{
					capi.ClusterNameLabel:      controlplane.Name,
					capr.ClusterNameLabel:      controlplane.Name,
					capr.EtcdRoleLabel:         "true",
					capr.ControlPlaneRoleLabel: "true",
					capr.WorkerRoleLabel:       "true",
				},
			},
			Spec: rkev1.RKEBootstrapSpec{
				ClusterName: controlplane.Name,
				Version:     controlplane.Spec.KubernetesVersion,
			},
		}
}

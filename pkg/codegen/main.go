package main

import (
	"os"

	"github.com/rancher/norman/types"
	"github.com/rancher/rancher/pkg/codegen/generator"
	"github.com/rancher/rancher/pkg/schemas/factory"
	controllergen "github.com/rancher/wrangler/v3/pkg/controller-gen"
	"github.com/rancher/wrangler/v3/pkg/controller-gen/args"
	appsv1 "k8s.io/api/apps/v1"
	scalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	knetworkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	capi "sigs.k8s.io/cluster-api/api/v1beta1"
)

func main() {
	os.Unsetenv("GOPATH")

	controllergen.Run(args.Options{
		OutputPackage: "github.com/rancher/cluster-api-provider-rancher/pkg/generated",
		Boilerplate:   "scripts/boilerplate.go.txt",
		Groups: map[string]args.Group{
			"rke.cattle.io": {
				Types: []interface{}{
					"./pkg/apis/rke.cattle.io/v1",
				},
				GenerateTypes:   true,
				GenerateClients: true,
			},
			"cluster.x-k8s.io": {
				Types: []interface{}{
					capi.Machine{},
					capi.MachineSet{},
					capi.MachineDeployment{},
					capi.MachineHealthCheck{},
					capi.Cluster{},
				},
			},
		},
	})

	clusterAPIVersion := &types.APIVersion{Group: capi.GroupVersion.Group, Version: capi.GroupVersion.Version, Path: "/v1"}
	generator.GenerateClient(factory.Schemas(clusterAPIVersion).Init(func(schemas *types.Schemas) *types.Schemas {
		return schemas.MustImportAndCustomize(clusterAPIVersion, capi.Machine{}, func(schema *types.Schema) {
			schema.ID = "cluster.x-k8s.io.machine"
		})
	}), nil)

	generator.GenerateNativeTypes(v1.SchemeGroupVersion, []interface{}{
		v1.Endpoints{},
		v1.PersistentVolumeClaim{},
		v1.Pod{},
		v1.Service{},
		v1.Secret{},
		v1.ConfigMap{},
		v1.ServiceAccount{},
		v1.ReplicationController{},
		v1.ResourceQuota{},
		v1.LimitRange{},
	}, []interface{}{
		v1.Node{},
		v1.ComponentStatus{},
		v1.Namespace{},
		v1.Event{},
	})
	generator.GenerateNativeTypes(appsv1.SchemeGroupVersion, []interface{}{
		appsv1.Deployment{},
		appsv1.DaemonSet{},
		appsv1.StatefulSet{},
		appsv1.ReplicaSet{},
	}, nil)
	generator.GenerateNativeTypes(rbacv1.SchemeGroupVersion, []interface{}{
		rbacv1.RoleBinding{},
		rbacv1.Role{},
	}, []interface{}{
		rbacv1.ClusterRoleBinding{},
		rbacv1.ClusterRole{},
	})
	generator.GenerateNativeTypes(knetworkingv1.SchemeGroupVersion, []interface{}{
		knetworkingv1.NetworkPolicy{},
		knetworkingv1.Ingress{},
	}, nil)
	generator.GenerateNativeTypes(batchv1.SchemeGroupVersion, []interface{}{
		batchv1.Job{},
		batchv1.CronJob{},
	}, nil)
	generator.GenerateNativeTypes(extv1beta1.SchemeGroupVersion,
		[]interface{}{
			extv1beta1.Ingress{},
		},
		nil,
	)
	generator.GenerateNativeTypes(storagev1.SchemeGroupVersion,
		nil,
		[]interface{}{
			storagev1.StorageClass{},
		},
	)
	generator.GenerateNativeTypes(scalingv2.SchemeGroupVersion,
		[]interface{}{
			scalingv2.HorizontalPodAutoscaler{},
		},
		nil,
	)
	generator.GenerateNativeTypes(apiregistrationv1.SchemeGroupVersion,
		nil,
		[]interface{}{
			apiregistrationv1.APIService{},
		},
	)
}

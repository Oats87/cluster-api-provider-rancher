package util

import (
	"context"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/collections"
	capifd "sigs.k8s.io/cluster-api/util/failuredomains"
)

// FailureDomainWithMostMachines returns a fd which exists both in machines and control-plane machines and has the most
// control-plane machines on it.
func FailureDomainWithMostMachines(ctx context.Context, cluster clusterv1.Cluster, clusterMachines []clusterv1.Machine, failureDomains clusterv1.FailureDomains, machines collections.Machines) *string {
	// See if there are any Machines that are not in currently defined failure domains first.
	notInFailureDomains := machines.Filter(
		collections.Not(collections.InFailureDomains(failureDomains.FilterControlPlane().GetIDs()...)),
	)
	if len(notInFailureDomains) > 0 {
		// return the failure domain for the oldest Machine not in the current list of failure domains
		// this could be either nil (no failure domain defined) or a failure domain that is no longer defined
		// in the cluster status.
		return notInFailureDomains.Oldest().Spec.FailureDomain
	}

	return capifd.PickMost(ctx, cluster.Status.FailureDomains.FilterControlPlane(), clusterMachines, machines)
}

// NextFailureDomainForScaleUp returns the failure domain with the fewest number of up-to-date machines.
func NextFailureDomainForScaleUp(ctx context.Context, failureDomains clusterv1.FailureDomains) *string {
	if len(failureDomains.FilterControlPlane()) == 0 {
		return nil
	}

	return capifd.PickFewest(ctx, failureDomains.FilterControlPlane(), c.UpToDateMachines())
}

// MachinesNeedingRollout return a list of machines that need to be rolled out.
func MachinesNeedingRollout(machines collections.Machines) collections.Machines {
	// Ignore machines to be deleted.

	// Return machines if they are scheduled for rollout or if with an outdated configuration.
	return machines.Filter(collections.Not(collections.HasDeletionTimestamp)).AnyFilter(
		// Machines that do not match with RCP config.
		collections.Not(matchesRCPConfiguration(c.infraResources, c.rke2Configs, c.RCP)),
	)
}

// matchesRCPConfiguration returns a filter to find all machines that matches with RCP config and do not require any rollout.
// Kubernetes version, infrastructure template, and RKE2Config field need to be equivalent.
func matchesRCPConfiguration(
	infraConfigs map[string]*unstructured.Unstructured,
	machineConfigs map[string]*bootstrapv1.RKE2Config,
	rcp *controlplanev1.RKE2ControlPlane,
) func(machine *clusterv1.Machine) bool {
	return collections.And(
		matchesKubernetesOrRKE2Version(rcp.GetDesiredVersion()),
		matchesRKE2BootstrapConfig(machineConfigs, rcp),
		matchesTemplateClonedFrom(infraConfigs, rcp),
	)
}

// UpToDateMachines returns the machines that are up to date with the control
// plane's configuration and therefore do not require rollout.
func UpToDateMachines(machines collections.Machines) collections.Machines {
	return machines.Difference(c.MachinesNeedingRollout())
}

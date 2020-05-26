package managedcompute

import (
	"context"
	"github.com/pkg/errors"
	"google.golang.org/api/container/v1"
	"net/url"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/gcperrors"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/wait"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileGKECluster creates the GKE cluster if it doesn't exist
func (s *Service) ReconcileGKECluster() error {
	// Reconcile GKE cluster
	spec := s.getGKESpec()
	cluster, err := s.clusters.Get(s.scope.ClusterRelativeName()).Do()
	if gcperrors.IsNotFound(err) {
		op, err := s.clusters.Create(s.scope.LocationRelativeName(), &container.CreateClusterRequest{
			Cluster: spec,
		}).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to create cluster")
		}
		if err := wait.ForContainerOperation(s.scope.Containers, s.scope.Project(), s.scope.Location(), op); err != nil {
			return errors.Wrapf(err, "failed to create cluster")
		}
		cluster, err = s.clusters.Get(s.scope.ClusterRelativeName()).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to describe cluster")
		}
	} else if err != nil {
		return errors.Wrapf(err, "failed to describe cluster")
	}

	// Reconcile endpoint
	oldControlPlane := s.scope.ControlPlane.DeepCopyObject()
	endpointURL, err := url.Parse(cluster.Endpoint)
	if err != nil {
		return errors.Wrapf(err, "failed to parse cluster master endpoint")
	}
	s.scope.ControlPlane.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
		Host: endpointURL.Hostname(),
		Port: 443,
	}

	if err := s.scope.Client.Patch(context.TODO(), s.scope.ControlPlane, client.MergeFrom(oldControlPlane)); err != nil {
		return errors.Wrapf(err, "failed to set control plane endpoint")
	}

	// TODO reconcile kubeconfig
	// Need to figure out the following things:
	// 1. How to get credentials? Do we want to use HTTP basic auth?
	// 2. How to generate kubeconfig? API doesn't return one.

	return nil
}

func (s *Service) getGKESpec() *container.Cluster {
	cluster := &container.Cluster{
		InitialClusterVersion: s.scope.KubernetesVersion(),
		IpAllocationPolicy: &container.IPAllocationPolicy{
			CreateSubnetwork: true,
			SubnetworkName:   s.scope.SubnetworkName(),
			UseIpAliases:     true,
		},
		Name:    s.scope.ControlPlane.Name,
		Network: s.scope.NetworkName(),
		NetworkPolicy: &container.NetworkPolicy{
			Enabled:  true,
			Provider: "CALICO",
		},
		NodePools: []*container.NodePool{
			{
				Autoscaling: &container.NodePoolAutoscaling{
					// TODO: autoscaling is currently not support
					Enabled:      false,
					MaxNodeCount: int64(*s.scope.MachinePool.Spec.Replicas),
					MinNodeCount: int64(*s.scope.MachinePool.Spec.Replicas),
				},
			},
		},
		ResourceLabels: s.scope.ControlPlane.Spec.AdditionalLabels,
		Subnetwork:     s.scope.SubnetworkName(),
	}

	return cluster
}

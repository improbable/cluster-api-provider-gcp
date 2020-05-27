/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package managedcompute

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/pkg/errors"
	"google.golang.org/api/container/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/gcperrors"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/wait"
	infrav1exp "sigs.k8s.io/cluster-api-provider-gcp/exp/api/v1alpha3"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ReconcileGKECluster creates the GKE cluster if it doesn't exist
func (s *Service) ReconcileGKECluster() error {
	ctx := context.Background()

	// Reconcile GKE cluster
	spec := s.getGKESpec()
	cluster, err := s.clusters.Get(s.scope.ClusterRelativeName()).Do()
	if gcperrors.IsNotFound(err) {
		s.scope.Logger.Info("GKE cluster not found, creating")
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

	oldControlPlane := s.scope.ControlPlane.DeepCopyObject()

	// Reconcile provider status
	s.scope.ControlPlane.Status.ProviderStatus = cluster.Status

	// Reconcile endpoint
	if cluster.Endpoint != "" {
		s.scope.ControlPlane.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
			Host: cluster.Endpoint,
			Port: 443,
		}
	}

	if err := s.scope.Client.Patch(context.TODO(), s.scope.ControlPlane, client.MergeFrom(oldControlPlane)); err != nil {
		return errors.Wrapf(err, "failed to set control plane endpoint")
	}

	// Reconcile kubeconfig
	if cluster.Endpoint != "" && cluster.MasterAuth.ClusterCaCertificate != "" && cluster.MasterAuth.Password != "" {
		clusterCaBytes, err := base64.StdEncoding.DecodeString(cluster.MasterAuth.ClusterCaCertificate)
		if err != nil {
			return errors.Wrapf(err, "failed to decode cluster CA certificate")
		}
		kubeconfig := clientcmdapi.Config{
			Clusters: map[string]*clientcmdapi.Cluster{
				s.scope.ControlPlane.Name: {
					Server:                   fmt.Sprintf("https://%s", cluster.Endpoint),
					CertificateAuthorityData: clusterCaBytes,
				},
			},
			AuthInfos: map[string]*clientcmdapi.AuthInfo{
				s.scope.ControlPlane.Name: {
					Username: cluster.MasterAuth.Username,
					Password: cluster.MasterAuth.Password,
				},
			},
			Contexts: map[string]*clientcmdapi.Context{
				s.scope.ControlPlane.Name: {
					Cluster: s.scope.ControlPlane.Name,
					AuthInfo: s.scope.ControlPlane.Name,
				},
			},
			CurrentContext: s.scope.ControlPlane.Name,
		}
		configYaml, err := clientcmd.Write(kubeconfig)
		if err != nil {
			return errors.Wrapf(err, "failed to write kubeconfig to yaml")
		}
		kubeconfigSecret := makeKubeconfig(s.scope.Cluster, s.scope.ControlPlane)
		if _, err := controllerutil.CreateOrUpdate(ctx, s.scope.Client, kubeconfigSecret, func() error {
			kubeconfigSecret.Data = map[string][]byte{
				secret.KubeconfigDataName: configYaml,
			}
			return nil
		}); err != nil {
			return errors.Wrapf(err, "failed to kubeconfig secret for cluster")
		}
	}

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
		MasterAuth: &container.MasterAuth{
			// Enable HTTP Basic Auth to allow the generation of a kubeconfig that bypasses Google OAuth
			// GKE client certs are broken and require additional config https://github.com/kubernetes/kubernetes/issues/65400
			Username: "admin",
		},
		Name:    s.scope.ControlPlane.Name,
		Network: s.scope.NetworkName(),
		NetworkPolicy: &container.NetworkPolicy{
			Enabled:  true,
			Provider: "CALICO",
		},
		NodePools: []*container.NodePool{
			s.getNodePoolSpec(),
		},
		ResourceLabels: s.scope.ControlPlane.Spec.AdditionalLabels,
	}

	return cluster
}

func (s *Service) getNodePoolSpec() *container.NodePool {
	return &container.NodePool{
		Autoscaling: &container.NodePoolAutoscaling{
			// TODO: autoscaling is currently not supported
			Enabled: false,
		},
		Config: s.getNodePoolConfig(),
		// For regional clusters, this is the node count per zone
		InitialNodeCount: s.scope.DefaultNodePoolReplicaCount(),
		Name:             s.scope.MachinePool.Name,
	}
}

func (s *Service) getNodePoolConfig() *container.NodeConfig {
	return &container.NodeConfig{
		DiskSizeGb:  s.scope.InfraMachinePool.Spec.BootDiskSizeGB,
		DiskType:    s.scope.InfraMachinePool.Spec.DiskType,
		MachineType: s.scope.InfraMachinePool.Spec.InstanceType,
		Preemptible: s.scope.InfraMachinePool.Spec.Preemptible,
	}
}

func makeKubeconfig(cluster *clusterv1.Cluster, controlPlane *infrav1exp.GCPManagedControlPlane) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(controlPlane, infrav1exp.GroupVersion.WithKind("GCPManagedControlPlane")),
			},
		},
	}
}

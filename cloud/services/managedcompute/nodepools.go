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
	"k8s.io/utils/pointer"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/gcperrors"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/wait"
	infrav1exp "sigs.k8s.io/cluster-api-provider-gcp/exp/api/v1alpha3"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (s *Service) ReconcileGKENodePool() error {
	// Get node pool to check for existence
	nodePool, err := s.nodepools.Get(s.scope.NodePoolRelativeName("default")).Do()
	// Create node pool if it does not exist
	if gcperrors.IsNotFound(err) {
		s.scope.Logger.Info("Node pool not found, creating")
		op, err := s.nodepools.Create(s.scope.ClusterRelativeName(), &container.CreateNodePoolRequest{
			NodePool: s.getNodePoolsSpec()[0],
		}).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to create node pool")
		}
		s.scope.Logger.Info("Waiting for operation", "op", op.Name)
		if err := wait.ForContainerOperation(s.scope.Containers, s.scope.Project(), s.scope.Location(), op); err != nil {
			return errors.Wrapf(err, "failed to create node pool")
		}
		s.scope.Logger.Info("Operation done", "op", op.Name)
		nodePool, err = s.nodepools.Get(s.scope.NodePoolRelativeName("default")).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to describe cluster")
		}
	}
	// TODO: Update node pool if it has been modified
	oldMachinePool := s.scope.InfraMachinePools["default"].DeepCopyObject()

	// Reconcile provider status
	s.scope.InfraMachinePools["default"].Status.ProviderStatus = nodePool.Status
	if nodePool.Status == "ERROR" || nodePool.Status == "RUNNING_WITH_ERROR" {
		s.scope.InfraMachinePools["default"].Status.ErrorMessage = pointer.StringPtr(nodePool.StatusMessage)
	}

	s.scope.Logger.Info("Patching machine pool status")
	if err := s.scope.Client.Patch(context.TODO(), s.scope.InfraMachinePools["default"], client.MergeFrom(oldMachinePool)); err != nil {
		return errors.Wrapf(err, "failed to patch infra machine pool")
	}

	return nil
}

func (s *Service) DeleteGKENodePool() error {
	_, err := s.nodepools.Get(s.scope.NodePoolRelativeName("default")).Do()
	if gcperrors.IsNotFound(err) {
		return nil
	}
	op, err := s.nodepools.Delete(s.scope.NodePoolRelativeName("default")).Do()
	if err != nil {
		return errors.Wrapf(err, "failed to delete nodepool")
	}
	s.scope.Logger.Info("Waiting for operation", "op", op.Name)
	if err := wait.ForContainerOperation(s.scope.Containers, s.scope.Project(), s.scope.Location(), op); err != nil {
		return errors.Wrapf(err, "failed to delete nodepool")
	}
	s.scope.Logger.Info("Operation done", "op", op.Name)
	_, err = s.nodepools.Get(s.scope.NodePoolRelativeName("default")).Do()
	if gcperrors.IsNotFound(err) {
		return nil
	}

	return errors.New("failed to delete cluster")
}

func (s *Service) getNodePoolsSpec() []*container.NodePool {
	var nodePools []*container.NodePool
	for machinePoolName := range s.scope.InfraMachinePools {
		nodePools = append(nodePools, &container.NodePool{
			Autoscaling: &container.NodePoolAutoscaling{
				// TODO: autoscaling is currently not supported
				Enabled: false,
			},
			Config: s.getNodePoolConfig(machinePoolName),
			// For regional clusters, this is the node count per zone
			InitialNodeCount: s.scope.NodePoolReplicaCount(machinePoolName),
			Name:             s.scope.MachinePools[machinePoolName].Name,
		})
	}
	return nodePools
}

func (s *Service) getNodePoolConfig(machinePoolName string) *container.NodeConfig {
	return &container.NodeConfig{
		DiskSizeGb:  s.scope.InfraMachinePools[machinePoolName].Spec.BootDiskSizeGB,
		DiskType:    s.scope.InfraMachinePools[machinePoolName].Spec.DiskType,
		MachineType: s.scope.InfraMachinePools[machinePoolName].Spec.InstanceType,
		Preemptible: s.scope.InfraMachinePools[machinePoolName].Spec.Preemptible,
	}
}

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
	"github.com/pkg/errors"
	"google.golang.org/api/container/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/gcperrors"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/wait"
)

func (s *Service) ReconcileGKENodePool(ctx context.Context) error {
	nodePoolName := s.scope.NodePoolRelativeName(s.scope.FirstInfraMachinePoolName())
	nodePoolSpec := s.getNodePoolsSpec()[0]
	// Get node pool to check for existence
	nodePool, err := s.nodepools.Get(nodePoolName).Context(ctx).Do()
	// Create node pool if it does not exist
	if gcperrors.IsNotFound(err) {
		s.scope.Logger.Info("Node pool not found, creating")
		op, err := s.nodepools.Create(s.scope.ClusterRelativeName(), &container.CreateNodePoolRequest{
			NodePool: nodePoolSpec,
		}).Context(ctx).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to create node pool")
		}
		s.scope.Logger.Info("Waiting for operation", "op", op.Name)
		if err := wait.ForContainerOperation(ctx, s.scope.Containers, s.scope.Project(), s.scope.Location(), op); err != nil {
			return errors.Wrapf(err, "failed to create node pool")
		}
		s.scope.Logger.Info("Operation done", "op", op.Name)
		nodePool, err = s.nodepools.Get(nodePoolName).Context(ctx).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to describe node pool")
		}
	} else if err != nil {
		return errors.Wrapf(err, "failed to describe node pool")
	}

	// TODO: Update node pool if it has been modified
	if nodePool.Autoscaling.Enabled != nodePoolSpec.Autoscaling.Enabled {
		return errors.Wrapf(err, "unable to enable/disable autoscaling after creation")
	}
	if nodePool.Autoscaling.Enabled {
		if nodePool.Autoscaling.MinNodeCount != nodePoolSpec.Autoscaling.MinNodeCount ||
			nodePool.Autoscaling.MaxNodeCount != nodePoolSpec.Autoscaling.MaxNodeCount {
			s.scope.Logger.Info("Autoscaling config changed, resizing")
			op, err := s.nodepools.SetAutoscaling(nodePoolName, &container.SetNodePoolAutoscalingRequest{
				Autoscaling: nodePoolSpec.Autoscaling,
			}).Context(ctx).Do()
			if err != nil {
				return errors.Wrapf(err, "failed to resize node pool")
			}
			s.scope.Logger.Info("Waiting for operation", "op", op.Name)
			if err := wait.ForContainerOperation(ctx, s.scope.Containers, s.scope.Project(), s.scope.Location(), op); err != nil {
				return errors.Wrapf(err, "failed to resize node pool")
			}
			s.scope.Logger.Info("Operation done", "op", op.Name)
			nodePool, err = s.nodepools.Get(nodePoolName).Context(ctx).Do()
			if err != nil {
				return errors.Wrapf(err, "failed to describe node pool")
			}
		}
	} else {
		if nodePool.InitialNodeCount != nodePoolSpec.InitialNodeCount {
			s.scope.Logger.Info("Node pool size changed, resizing")
			op, err := s.nodepools.SetSize(nodePoolName, &container.SetNodePoolSizeRequest{
				NodeCount: nodePoolSpec.InitialNodeCount,
			}).Context(ctx).Do()
			if err != nil {
				return errors.Wrapf(err, "failed to resize node pool")
			}
			s.scope.Logger.Info("Waiting for operation", "op", op.Name)
			if err := wait.ForContainerOperation(ctx, s.scope.Containers, s.scope.Project(), s.scope.Location(), op); err != nil {
				return errors.Wrapf(err, "failed to resize node pool")
			}
			s.scope.Logger.Info("Operation done", "op", op.Name)
			nodePool, err = s.nodepools.Get(nodePoolName).Context(ctx).Do()
			if err != nil {
				return errors.Wrapf(err, "failed to describe node pool")
			}
		}
	}

	// Reconcile provider status
	s.scope.FirstInfraMachinePool().Status.ProviderStatus = nodePool.Status
	if nodePool.Status == "ERROR" || nodePool.Status == "RUNNING_WITH_ERROR" {
		s.scope.FirstInfraMachinePool().Status.ErrorMessage = pointer.StringPtr(nodePool.StatusMessage)
	} else {
		s.scope.FirstInfraMachinePool().Status.ErrorMessage = nil
	}

	return nil
}

func (s *Service) DeleteGKENodePool(ctx context.Context) error {
	nodePoolName := s.scope.NodePoolRelativeName(s.scope.FirstInfraMachinePoolName())
	_, err := s.nodepools.Get(nodePoolName).Context(ctx).Do()
	if gcperrors.IsNotFound(err) {
		return nil
	}
	op, err := s.nodepools.Delete(nodePoolName).Context(ctx).Do()
	if err != nil {
		return errors.Wrapf(err, "failed to delete nodepool")
	}
	s.scope.Logger.Info("Waiting for operation", "op", op.Name)
	if err := wait.ForContainerOperation(ctx, s.scope.Containers, s.scope.Project(), s.scope.Location(), op); err != nil {
		return errors.Wrapf(err, "failed to delete nodepool")
	}
	s.scope.Logger.Info("Operation done", "op", op.Name)
	_, err = s.nodepools.Get(nodePoolName).Context(ctx).Do()
	if gcperrors.IsNotFound(err) {
		return nil
	}

	return errors.New("failed to delete cluster")
}

func (s *Service) getNodePoolsSpec() []*container.NodePool {
	nodePools := make([]*container.NodePool, len(s.scope.InfraMachinePools))
	for machinePoolName, machinePool := range s.scope.InfraMachinePools {
		nodePool := &container.NodePool{
			Config: s.getNodePoolConfig(machinePoolName),
			Name:             machinePool.Name,
		}
		if machinePool.Spec.Autoscaling != nil {
			nodePool.Autoscaling = &container.NodePoolAutoscaling{
				Enabled: true,
				MinNodeCount: machinePool.Spec.Autoscaling.MinNodeCount,
				MaxNodeCount: machinePool.Spec.Autoscaling.MaxNodeCount,
			}
		} else {
			// For regional clusters, this is the node count per zone
			nodePool.InitialNodeCount = s.scope.NodePoolReplicaCount(machinePoolName)
		}
		nodePools = append(nodePools, nodePool)
	}
	return nodePools
}

func (s *Service) getNodePoolConfig(machinePoolName string) *container.NodeConfig {
	var gcpTaints []*container.NodeTaint
	for _, taint := range s.scope.InfraMachinePools[machinePoolName].Spec.Taints {
		gcpTaints = append(gcpTaints, &container.NodeTaint{
			Effect: string(taint.Effect),
			Key: taint.Key,
			Value: taint.Value,
		})
	}
	return &container.NodeConfig{
		DiskSizeGb:  s.scope.InfraMachinePools[machinePoolName].Spec.BootDiskSizeGB,
		DiskType:    s.scope.InfraMachinePools[machinePoolName].Spec.DiskType,
		MachineType: s.scope.InfraMachinePools[machinePoolName].Spec.InstanceType,
		Preemptible: s.scope.InfraMachinePools[machinePoolName].Spec.Preemptible,
		Labels:      s.scope.InfraMachinePools[machinePoolName].Spec.Labels,
		Taints:      gcpTaints,
	}
}

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

package scope

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/klogr"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/exp/api/v1alpha3"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ManagedControlPlaneScopeParams defines the input parameters used to create a new Scope.
type ManagedControlPlaneScopeParams struct {
	GCPClients
	Client            client.Client
	Logger            logr.Logger
	Cluster           *clusterv1.Cluster
	ControlPlane      *infrav1.GCPManagedControlPlane
	InfraMachinePools map[string]*infrav1.GCPManagedMachinePool
	MachinePools      map[string]*expv1.MachinePool
	PatchTarget       runtime.Object
}

// NewManagedControlPlaneScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewManagedControlPlaneScope(params ManagedControlPlaneScopeParams) (*ManagedControlPlaneScope, error) {
	if params.Logger == nil {
		params.Logger = klogr.New()
	}

	computeSvc, err := compute.NewService(context.TODO())
	if err != nil {
		return nil, errors.Errorf("failed to create gcp compute client: %v", err)
	}

	if params.GCPClients.Compute == nil {
		params.GCPClients.Compute = computeSvc
	}

	containerSvc, err := container.NewService(context.TODO())
	if err != nil {
		return nil, errors.Errorf("failed to create gcp container client: %v", err)
	}

	if params.GCPClients.Containers == nil {
		params.GCPClients.Containers = containerSvc
	}

	helper, err := patch.NewHelper(params.PatchTarget, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}
	return &ManagedControlPlaneScope{
		Logger:            params.Logger,
		Client:            params.Client,
		GCPClients:        params.GCPClients,
		Cluster:           params.Cluster,
		ControlPlane:      params.ControlPlane,
		InfraMachinePools: params.InfraMachinePools,
		MachinePools:      params.MachinePools,
		PatchTarget:       params.PatchTarget,
		patchHelper:       helper,
	}, nil
}

// ManagedControlPlaneScope defines the basic context for an actuator to operate upon.
type ManagedControlPlaneScope struct {
	logr.Logger
	Client      client.Client
	patchHelper *patch.Helper

	GCPClients
	Cluster           *clusterv1.Cluster
	ControlPlane      *infrav1.GCPManagedControlPlane
	InfraMachinePools map[string]*infrav1.GCPManagedMachinePool
	MachinePools      map[string]*expv1.MachinePool
	PatchTarget       runtime.Object
}

// PatchObject persists the cluster configuration and status.
func (s *ManagedControlPlaneScope) PatchObject(ctx context.Context) error {
	return s.patchHelper.Patch(ctx, s.PatchTarget)
}

// Name returns the cluster name.
func (s *ManagedControlPlaneScope) Name() string {
	return s.Cluster.Name
}

// Project returns the current project name.
func (s *ManagedControlPlaneScope) Project() string {
	return s.ControlPlane.Spec.Project
}

// NetworkName returns the name of the network used by the cluster.
func (s *ManagedControlPlaneScope) NetworkName() string {
	if s.ControlPlane.Spec.Network.Name == nil {
		return "default"
	}
	return *s.ControlPlane.Spec.Network.Name
}

// SubnetworkName returns the name of the subnetwork used by the cluster.
func (s *ManagedControlPlaneScope) SubnetworkName() string {
	if s.ControlPlane.Spec.Network.Name == nil {
		return ""
	}
	return *s.ControlPlane.Spec.Network.Subnetwork
}

// KubernetesVersion returns the initial kubernetes version to start the cluster with.
func (s *ManagedControlPlaneScope) KubernetesVersion() string {
	return s.ControlPlane.Spec.Version
}

func (s *ManagedControlPlaneScope) Location() string {
	return s.ControlPlane.Spec.Region
}

func (s *ManagedControlPlaneScope) LocationRelativeName() string {
	return fmt.Sprintf("projects/%s/locations/%s", s.Project(), s.Location())
}

func (s *ManagedControlPlaneScope) ClusterRelativeName() string {
	return fmt.Sprintf("%s/clusters/%s", s.LocationRelativeName(), s.ControlPlane.Name)
}

func (s *ManagedControlPlaneScope) NodePoolRelativeName(machinePoolName string) string {
	return fmt.Sprintf("%s/nodePools/%s", s.ClusterRelativeName(), s.InfraMachinePools[machinePoolName].Name)
}

func (s *ManagedControlPlaneScope) NodePoolReplicaCount(machinePoolName string) int64 {
	if s.MachinePools[machinePoolName].Spec.Replicas == nil {
		return 1
	}
	return int64(*s.MachinePools[machinePoolName].Spec.Replicas)
}

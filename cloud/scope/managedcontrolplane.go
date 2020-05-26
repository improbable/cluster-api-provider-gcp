/*
Copyright 2018 The Kubernetes Authors.

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
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1alpha3"
)

// ManagedControlPlaneScopeParams defines the input parameters used to create a new Scope.
type ManagedControlPlaneScopeParams struct {
	GCPClients
	Client           client.Client
	Logger           logr.Logger
	Cluster          *clusterv1.Cluster
	ControlPlane     *infrav1.GCPManagedControlPlane
	InfraMachinePool *infrav1.GCPManagedMachinePool
	MachinePool      *expv1.MachinePool
	PatchTarget      runtime.Object
}

// NewManagedControlPlaneScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewManagedControlPlaneScope(params ManagedControlPlaneScopeParams) (*ManagedControlPlaneScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.ControlPlane == nil {
		return nil, errors.New("failed to generate new scope from nil ControlPlane")
	}

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
		Logger:           params.Logger,
		client:           params.Client,
		GCPClients:       params.GCPClients,
		Cluster:          params.Cluster,
		ControlPlane:     params.ControlPlane,
		InfraMachinePool: params.InfraMachinePool,
		MachinePool:      params.MachinePool,
		PatchTarget:      params.PatchTarget,
		patchHelper:      helper,
	}, nil
}

// ManagedControlPlaneScope defines the basic context for an actuator to operate upon.
type ManagedControlPlaneScope struct {
	logr.Logger
	client      client.Client
	patchHelper *patch.Helper

	GCPClients
	Cluster          *clusterv1.Cluster
	ControlPlane     *infrav1.GCPManagedControlPlane
	InfraMachinePool *infrav1.GCPManagedMachinePool
	MachinePool      *expv1.MachinePool
	PatchTarget      runtime.Object
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

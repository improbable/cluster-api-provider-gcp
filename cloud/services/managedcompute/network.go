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

package compute

import (
	"github.com/pkg/errors"
	"google.golang.org/api/compute/v1"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1alpha3"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/gcperrors"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/wait"
)

// ReconcileNetwork creates the VPC network if it doesn't exist
func (s *Service) ReconcileNetwork() error {
	// Create Network
	spec := s.getNetworkSpec()
	_, err := s.networks.Get(s.scope.Project(), spec.Name).Do()
	if gcperrors.IsNotFound(err) {
		op, err := s.networks.Insert(s.scope.Project(), spec).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to create network")
		}
		if err := wait.ForComputeOperation(s.scope.Compute, s.scope.Project(), op); err != nil {
			return errors.Wrapf(err, "failed to create network")
		}
		_, err = s.networks.Get(s.scope.Project(), spec.Name).Do()
		if err != nil {
			return errors.Wrapf(err, "failed to describe network")
		}
	} else if err != nil {
		return errors.Wrapf(err, "failed to describe network")
	}

	// TODO: Create the subnetwork as well

	return nil
}

func (s *Service) getNetworkSpec() *compute.Network {
	res := &compute.Network{
		Name:                  s.scope.NetworkName(),
		Description:           infrav1.ClusterTagKey(s.scope.Name()),
		AutoCreateSubnetworks: false,
	}

	return res
}

func (s *Service) DeleteNetwork() error {
	network, err := s.networks.Get(s.scope.Project(), s.scope.NetworkName()).Do()
	if gcperrors.IsNotFound(err) {
		return nil
	}

	// Return early if the description doesn't match our ownership tag.
	if network.Description != infrav1.ClusterTagKey(s.scope.Name()) {
		return nil
	}

	// Delete Network.
	op, err := s.networks.Delete(s.scope.Project(), network.Name).Do()
	if err != nil {
		return errors.Wrapf(err, "failed to delete forwarding rules")
	}
	if err := wait.ForComputeOperation(s.scope.Compute, s.scope.Project(), op); err != nil {
		return errors.Wrapf(err, "failed to delete forwarding rules")
	}
	s.scope.ControlPlane.Spec.Network.Name = nil
	return nil
}

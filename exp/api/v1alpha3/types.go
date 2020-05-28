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

package v1alpha3

type ManagedNetworkSpec struct {
	// Name is the name of the network to be used.
	// If empty, defaults to the default network.
	// +optional
	Name *string `json:"name,omitempty"`

	// Subnetwork is the name of the subnetwork to be used.
	// If empty, an automatic name will be used.
	// +optional
	Subnetwork *string `json:"subnetwork,omitempty"`
}

type AutoscalingSpec struct {
	// MaxNodeCount: Maximum number of nodes in the NodePool. Must be >= minNodeCount.
	// +kubebuilder:validation:Minimum=1
	MaxNodeCount int64 `json:"maxNodeCount"`

	// MinNodeCount: Minimum number of nodes in the NodePool. Must be >= 1 and <= maxNodeCount.
	// +kubebuilder:validation:Minimum=1
	MinNodeCount int64 `json:"minNodeCount"`
}

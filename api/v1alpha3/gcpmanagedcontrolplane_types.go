/*
Copyright 2019 The Kubernetes Authors.

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

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
)

const (
	// ManagedControlPlaneFinalizer allows ReconcileGCPManagedControlPlane to clean up GCP resources associated with
	// ReconcileGCPManagedControlPlane before removing it from the apiserver.
	ManagedControlPlaneFinalizer = "gcpmanagedcontrolplane.infrastructure.cluster.x-k8s.io"
)

// GCPManagedControlPlaneSpec defines the desired state of GCPManagedControlPlane
type GCPManagedControlPlaneSpec struct {
	// Version defines the desired Kubernetes version.
	// +kubebuilder:validation:MinLength:=2
	// +kubebuilder:validation:Pattern:=^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)([-0-9a-zA-Z_\.+]*)?$
	Version string `json:"version"`

	// Project is the name of the GCP project to start the GKE cluster in.
	Project string `json:"project"`

	// Region is a string matching one of the canonical GCP region names. Examples: "us-central1", "europe-west1".
	Region string `json:"region"`

	// Network encapsulates all things related to GCP network.
	// +optional
	Network *ManagedNetworkSpec `json:"network,omitempty"`

	// ControlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint"`

	// AdditionalLabels is an optional set of labels to add to GCP resources managed by the GCP provider, in addition to the
	// ones added by default.
	// +optional
	AdditionalLabels map[string]string `json:"additionalLabels,omitempty"`

	// DefaultPoolRef is the specification for the default pool, without which an GKE cluster cannot be created.
	DefaultPoolRef corev1.LocalObjectReference `json:"defaultPoolRef"`
}

// GCPManagedControlPlaneStatus defines the observed state of GCPManagedControlPlane
type GCPManagedControlPlaneStatus struct {
	// Ready is true when the provider resource is ready.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// Initialized is true when the the control plane is available for initial contact.
	// This may occur before the control plane is fully ready.
	// In the GCPManagedControlPlane implementation, these are identical.
	// +optional
	Initialized bool `json:"initialized,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=gcpmanagedcontrolplanes,scope=Namespaced,categories=cluster-api,shortName=gmcp
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// GCPManagedControlPlane is the Schema for the gcpmanagedcontrolplanes API
type GCPManagedControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GCPManagedControlPlaneSpec   `json:"spec,omitempty"`
	Status GCPManagedControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GCPManagedControlPlaneList contains a list of GCPManagedControlPlane
type GCPManagedControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GCPManagedControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GCPManagedControlPlane{}, &GCPManagedControlPlaneList{})
}

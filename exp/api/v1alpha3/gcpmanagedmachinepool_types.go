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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	capierrors "sigs.k8s.io/cluster-api/errors"
)

const (
	// ManagedMachinePoolFinalizer allows ReconcileGCPManagedMachinePool to clean up GCP resources associated with #
	// GCPManagedMachinePool before removing it from the apiserver.
	ManagedMachinePoolFinalizer = "gcpmanagedmachinepool.exp.infrastructure.cluster.x-k8s.io"
)

// GCPManagedMachinePoolSpec defines the desired state of GCPManagedMachinePool
type GCPManagedMachinePoolSpec struct {
	// InstanceType is the type of the VMs in the node pool.
	InstanceType string `json:"instanceType"`

	// BootDiskSizeGB is the disk size for every machine in this pool.
	// If you specify 0, it will apply the default size.
	// +optional
	BootDiskSizeGB int64 `json:"bootDiskSizeGB,omitempty"`

	// DiskType is the type of boot disk for machines in this pool.
	// Possible values include: 'pd-standard', 'pd-ssd'. Defaults to 'pd-standard'.
	// +kubebuilder:validation:Enum=pd-standard;pd-ssd
	// +optional
	DiskType string `json:"diskType,omitempty"`

	// Preemptible determines if the nodes are preemptible. Defaults to false.
	// +optional
	Preemptible bool `json:"preemptible,omitempty"`
}

// GCPManagedMachinePoolStatus defines the observed state of GCPManagedMachinePool
type GCPManagedMachinePoolStatus struct {
	// Ready is true when the provider resource is ready.
	// +optional
	Ready bool `json:"ready"`

	// Replicas is the most recently observed number of replicas.
	// +optional
	Replicas int32 `json:"replicas"`

	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	ErrorReason *capierrors.MachineStatusError `json:"errorReason,omitempty"`

	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	ErrorMessage *string `json:"errorMessage,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=gcpmanagedmachinepools,scope=Namespaced,categories=cluster-api,shortName=gmmp
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// GCPManagedMachinePool is the Schema for the gcpmanagedmachinepools API
type GCPManagedMachinePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GCPManagedMachinePoolSpec   `json:"spec,omitempty"`
	Status GCPManagedMachinePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GCPManagedMachinePoolList contains a list of GCPManagedMachinePool
type GCPManagedMachinePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GCPManagedMachinePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GCPManagedMachinePool{}, &GCPManagedMachinePoolList{})
}

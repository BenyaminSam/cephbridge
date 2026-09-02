/*
Copyright 2026.

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

package v1alpha1

import (
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type CephVolumeSnapshotSource struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type CephVolumeSource struct {
	// +optional
	Snapshot *CephVolumeSnapshotSource `json:"snapshot,omitempty"`
}

// CephVolumeSpec defines the desired state of CephVolume
type CephVolumeSpec struct {

	// +kubebuilder:validation:MinLength=1
	Pool string `json:"pool"`

	// // +optional
	// ImageName string `json:"imageName,omitempty"`

	// +required
	Size resource.Quantity `json:"size"`

	// +optional
	Features []string `json:"features,omitempty"`

	// +optional
	Source *CephVolumeSource `json:"source,omitempty"`

	// +kubebuilder:default=Delete
	// +kubebuilder:validation=Enum=Delete,Retain
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

// CephVolumeStatus defines the observed state of CephVolume.
type CephVolumeStatus struct {

	// +optional
	Phase string `json:"phase,omitempty"`

	// +optional
	Message string `json:"message,omitempty"`

	// +optional
	ImageName string `json:"imageName,omitempty"`

	// +optional
	Pool string `json:"pool,omitempty"`

	// +optional
	Size string `json:"size,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	PVCName string `json:"pvcName,omitempty"`

	// +optional
	PVName string `json:"pvName,omitempty"`

	// +optional
	VolumeHandle string `json:"volumeHandle,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CephVolume is the Schema for the cephvolumes API
type CephVolume struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CephVolume
	// +required
	Spec CephVolumeSpec `json:"spec"`

	// status defines the observed state of CephVolume
	// +optional
	Status CephVolumeStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true
// CephVolumeList contains a list of CephVolume
type CephVolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CephVolume `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &CephVolume{}, &CephVolumeList{})
		return nil
	})
}

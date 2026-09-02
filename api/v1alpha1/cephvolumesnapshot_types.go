package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type CephVolumeSnapshotSpec struct {

	//+kubebuilder:validation:MinLength=1
	VolumeName string `json:"volumeName"`

	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;Retain
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

type CephVolumeSnapshotStatus struct {

	// +optional
	Phase string `json:"phase,omitempty"`

	// +optional
	Message string `json:"message,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	VolumeSnapshotName string `json:"volumeSnapshotname,omitempty"`

	// +optional
	VolumeSnapshotContentName string `json:"volumeSnapshotContentName,omitempty"`

	// +optional
	SnapshotHandle string `json:"snapshotHandle,omitempty"`

	// +optional
	ReadyToUse bool `json:"readyToUse,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type CephVolumeSnapshot struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec CephVolumeSnapshotSpec `json:"spec"`

	// +optional
	Status CephVolumeSnapshotStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true
type CephVolumeSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CephVolumeSnapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &CephVolumeSnapshot{}, &CephVolumeSnapshotList{})
		return nil
	})
}

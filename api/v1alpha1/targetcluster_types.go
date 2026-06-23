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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TargetClusterType discriminates the provisioning mode of a TargetCluster.
// Only "byo" is implemented in v1alpha1; other values are reserved for future
// provider integrations (k3s, rke2, gke, eks, aks).
// +kubebuilder:validation:Enum=byo
type TargetClusterType string

const (
	TargetClusterTypeBYO TargetClusterType = "byo"
)

// TargetClusterSpec defines the desired state of TargetCluster.
type TargetClusterSpec struct {
	// type selects the provisioning mode for the target cluster.
	// +kubebuilder:validation:Required
	Type TargetClusterType `json:"type"`

	// byo configures a Bring-Your-Own target cluster. Must be set when type is "byo".
	// +optional
	BYO *BYOSpec `json:"byo,omitempty"`
}

// BYOSpec configures a target cluster the user has already provisioned.
type BYOSpec struct {
	// kubeconfigSecretRef points to a Secret in the same namespace as the
	// TargetCluster containing a kubeconfig under .data[key].
	// +kubebuilder:validation:Required
	KubeconfigSecretRef KubeconfigSecretRef `json:"kubeconfigSecretRef"`
}

// KubeconfigSecretRef references a kubeconfig stored in a Secret.
type KubeconfigSecretRef struct {
	// name is the Secret name in the same namespace as the TargetCluster.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// key is the data key within the Secret that holds the kubeconfig bytes.
	// +kubebuilder:default=kubeconfig
	// +optional
	Key string `json:"key,omitempty"`
}

// TargetClusterStatus defines the observed state of TargetCluster.
type TargetClusterStatus struct {
	// ready reports whether the operator successfully reached the target cluster
	// on its last reconcile.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// kubernetesVersion is the server version reported by the target cluster.
	// +optional
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`

	// nodeCount is the number of Nodes observed on the target cluster.
	// +optional
	NodeCount int32 `json:"nodeCount,omitempty"`

	// observedGeneration is the .metadata.generation the status was last reconciled against.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the TargetCluster resource.
	// Standard condition types: "Available", "Progressing", "Degraded".
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.kubernetesVersion`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TargetCluster is the Schema for the targetclusters API
type TargetCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TargetCluster
	// +required
	Spec TargetClusterSpec `json:"spec"`

	// status defines the observed state of TargetCluster
	// +optional
	Status TargetClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TargetClusterList contains a list of TargetCluster
type TargetClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TargetCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TargetCluster{}, &TargetClusterList{})
		return nil
	})
}

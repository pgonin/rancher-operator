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

// LocalObjectReference points to an object in the same namespace by name.
type LocalObjectReference struct {
	// name of the referenced object.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// IngressType enumerates supported ingress controllers.
// +kubebuilder:validation:Enum=traefik
type IngressType string

const (
	IngressTypeTraefik IngressType = "traefik"
)

// LoadBalancerType enumerates supported load-balancer strategies.
// +kubebuilder:validation:Enum=cloud;metallb;none
type LoadBalancerType string

const (
	// LoadBalancerTypeCloud relies on the cluster's cloud-controller-manager; nothing is installed.
	LoadBalancerTypeCloud LoadBalancerType = "cloud"
	// LoadBalancerTypeMetalLB installs MetalLB on the target cluster.
	LoadBalancerTypeMetalLB LoadBalancerType = "metallb"
	// LoadBalancerTypeNone means the user has provisioned their own load balancer.
	LoadBalancerTypeNone LoadBalancerType = "none"
)

// ClusterPrerequisitesSpec defines the desired state of ClusterPrerequisites.
type ClusterPrerequisitesSpec struct {
	// targetClusterRef points to a TargetCluster in the same namespace where the
	// prerequisite components will be installed.
	// +kubebuilder:validation:Required
	TargetClusterRef LocalObjectReference `json:"targetClusterRef"`

	// certManager configures the cert-manager installation. If omitted, cert-manager
	// is not managed by this resource and is reported as skipped.
	// +optional
	CertManager *CertManagerComponent `json:"certManager,omitempty"`

	// ingress configures the ingress controller installation. If omitted, no ingress
	// controller is managed and the component is reported as skipped.
	// +optional
	Ingress *IngressComponent `json:"ingress,omitempty"`

	// loadBalancer configures the load-balancer strategy. If omitted, the load
	// balancer is reported as skipped.
	// +optional
	LoadBalancer *LoadBalancerComponent `json:"loadBalancer,omitempty"`
}

// CertManagerComponent configures cert-manager installation.
type CertManagerComponent struct {
	// enabled controls whether cert-manager is installed.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// version pins the cert-manager Helm chart version. Defaults to a value
	// chosen by the operator if unset.
	// +optional
	Version string `json:"version,omitempty"`
}

// IngressComponent configures the ingress-controller installation.
type IngressComponent struct {
	// enabled controls whether the ingress controller is installed.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// type selects which ingress controller to install. Currently only "traefik"
	// is supported.
	// +kubebuilder:default=traefik
	// +optional
	Type IngressType `json:"type,omitempty"`

	// version pins the ingress controller Helm chart version. Defaults to a value
	// chosen by the operator if unset.
	// +optional
	Version string `json:"version,omitempty"`
}

// LoadBalancerComponent configures the load-balancer strategy.
type LoadBalancerComponent struct {
	// type selects the load-balancer strategy.
	// +kubebuilder:validation:Required
	Type LoadBalancerType `json:"type"`

	// metallb carries MetalLB-specific configuration. Required when type is "metallb".
	// +optional
	MetalLB *MetalLBConfig `json:"metallb,omitempty"`
}

// MetalLBConfig configures the MetalLB single-pool L2 installation.
type MetalLBConfig struct {
	// addressPool is the address range MetalLB will advertise via L2.
	// Accepts either CIDR notation ("192.168.1.0/24") or a hyphenated range
	// ("192.168.1.240-192.168.1.250").
	// +kubebuilder:validation:Required
	AddressPool string `json:"addressPool"`

	// version pins the MetalLB Helm chart version. Defaults to a value chosen
	// by the operator if unset.
	// +optional
	Version string `json:"version,omitempty"`
}

// ComponentStatus reports the install state of a single prerequisite component.
type ComponentStatus struct {
	// ready is true when the component is installed and healthy on the target cluster.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// skipped is true when the component was disabled in spec or detected as
	// pre-existing and therefore not managed by this resource.
	// +optional
	Skipped bool `json:"skipped,omitempty"`

	// installedVersion is the chart/version actually deployed on the target cluster.
	// +optional
	InstalledVersion string `json:"installedVersion,omitempty"`

	// message carries a human-readable description of the component's current state.
	// +optional
	Message string `json:"message,omitempty"`
}

// ComponentStatuses aggregates per-component install state.
type ComponentStatuses struct {
	// +optional
	CertManager ComponentStatus `json:"certManager,omitzero"`
	// +optional
	Ingress ComponentStatus `json:"ingress,omitzero"`
	// +optional
	LoadBalancer ComponentStatus `json:"loadBalancer,omitzero"`
}

// ClusterPrerequisitesStatus defines the observed state of ClusterPrerequisites.
type ClusterPrerequisitesStatus struct {
	// ready is true when all non-skipped components are installed and healthy.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// components reports per-component install state.
	// +optional
	Components ComponentStatuses `json:"components,omitzero"`

	// observedGeneration is the .metadata.generation the status was last reconciled against.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the ClusterPrerequisites resource.
	// Standard condition types: "Available", "Progressing", "Degraded".
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetClusterRef.name`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterPrerequisites is the Schema for the clusterprerequisites API
type ClusterPrerequisites struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ClusterPrerequisites
	// +required
	Spec ClusterPrerequisitesSpec `json:"spec"`

	// status defines the observed state of ClusterPrerequisites
	// +optional
	Status ClusterPrerequisitesStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ClusterPrerequisitesList contains a list of ClusterPrerequisites
type ClusterPrerequisitesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ClusterPrerequisites `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ClusterPrerequisites{}, &ClusterPrerequisitesList{})
		return nil
	})
}

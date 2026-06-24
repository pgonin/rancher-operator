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

// TLSSource enumerates strategies for provisioning Rancher Manager's TLS cert.
// Mirrors the Rancher Helm chart's `ingress.tls.source` values.
// +kubebuilder:validation:Enum=rancher;letsEncrypt;secret
type TLSSource string

const (
	// TLSSourceRancher uses a Rancher-generated self-signed CA.
	TLSSourceRancher TLSSource = "rancher"
	// TLSSourceLetsEncrypt requests certs from Let's Encrypt via cert-manager (HTTP-01).
	TLSSourceLetsEncrypt TLSSource = "letsEncrypt"
	// TLSSourceSecret uses a user-provided kubernetes.io/tls Secret.
	TLSSourceSecret TLSSource = "secret"
)

// LetsEncryptEnv selects between ACME endpoints.
// +kubebuilder:validation:Enum=production;staging
type LetsEncryptEnv string

const (
	LetsEncryptEnvProduction LetsEncryptEnv = "production"
	LetsEncryptEnvStaging    LetsEncryptEnv = "staging"
)

// RancherManagerPhase summarizes the install lifecycle for UI consumption.
// +kubebuilder:validation:Enum=Pending;Installing;Available;Upgrading;Degraded
type RancherManagerPhase string

const (
	RancherManagerPhasePending    RancherManagerPhase = "Pending"
	RancherManagerPhaseInstalling RancherManagerPhase = "Installing"
	RancherManagerPhaseAvailable  RancherManagerPhase = "Available"
	RancherManagerPhaseUpgrading  RancherManagerPhase = "Upgrading"
	RancherManagerPhaseDegraded   RancherManagerPhase = "Degraded"
)

// RancherManagerSpec defines the desired state of RancherManager.
type RancherManagerSpec struct {
	// targetClusterRef points to a TargetCluster in the same namespace where
	// Rancher Manager will be installed.
	// +kubebuilder:validation:Required
	TargetClusterRef LocalObjectReference `json:"targetClusterRef"`

	// prerequisitesRef optionally references a ClusterPrerequisites resource
	// that must be Ready before installing or upgrading Rancher Manager.
	// +optional
	PrerequisitesRef *LocalObjectReference `json:"prerequisitesRef,omitempty"`

	// version is the Rancher Manager Helm chart version to deploy, e.g. "2.10.1".
	// Edit this field to trigger an upgrade.
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// hostname is the DNS name at which Rancher Manager will be reachable.
	// +kubebuilder:validation:Required
	Hostname string `json:"hostname"`

	// tls configures the TLS source for the Rancher ingress.
	// +kubebuilder:validation:Required
	TLS RancherTLS `json:"tls"`

	// bootstrapPasswordSecretRef references a Secret in the same namespace
	// holding the initial admin bootstrap password.
	// +kubebuilder:validation:Required
	BootstrapPasswordSecretRef BootstrapPasswordSecretRef `json:"bootstrapPasswordSecretRef"`

	// ingressClass attached to Rancher's Ingress. Defaults to "traefik" to match
	// the ClusterPrerequisites default.
	// +kubebuilder:default=traefik
	// +optional
	IngressClass string `json:"ingressClass,omitempty"`

	// replicas controls how many Rancher Manager pods to run.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
}

// RancherTLS configures TLS for the Rancher Manager ingress.
type RancherTLS struct {
	// source selects the TLS strategy.
	// +kubebuilder:validation:Required
	Source TLSSource `json:"source"`

	// letsEncrypt configures ACME issuance. Required when source is "letsEncrypt".
	// +optional
	LetsEncrypt *LetsEncryptConfig `json:"letsEncrypt,omitempty"`

	// secret references a kubernetes.io/tls Secret in the same namespace.
	// Required when source is "secret".
	// +optional
	Secret *LocalObjectReference `json:"secret,omitempty"`
}

// LetsEncryptConfig carries ACME registration details.
type LetsEncryptConfig struct {
	// email registered with the ACME provider.
	// +kubebuilder:validation:Required
	Email string `json:"email"`

	// environment selects the production or staging ACME endpoint.
	// +kubebuilder:default=production
	// +optional
	Environment LetsEncryptEnv `json:"environment,omitempty"`
}

// BootstrapPasswordSecretRef references a Secret carrying the initial admin password.
type BootstrapPasswordSecretRef struct {
	// name is the Secret name in the same namespace as the RancherManager.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// key within the Secret holding the password bytes.
	// +kubebuilder:default=password
	// +optional
	Key string `json:"key,omitempty"`
}

// RancherManagerStatus defines the observed state of RancherManager.
type RancherManagerStatus struct {
	// phase summarizes the install lifecycle.
	// +optional
	Phase RancherManagerPhase `json:"phase,omitempty"`

	// installedVersion is the chart version currently deployed on the target cluster.
	// +optional
	InstalledVersion string `json:"installedVersion,omitempty"`

	// availableUpgrades lists chart versions newer than installedVersion that the
	// operator has observed in the upstream Rancher Helm repository.
	// +optional
	AvailableUpgrades []string `json:"availableUpgrades,omitempty"`

	// url is the full URL of the Rancher Manager UI/API once available.
	// +optional
	URL string `json:"url,omitempty"`

	// observedGeneration is the .metadata.generation the status was last reconciled against.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the RancherManager resource.
	// Standard condition types: "Available", "Progressing", "Degraded".
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetClusterRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.installedVersion`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// RancherManager is the Schema for the ranchermanagers API
type RancherManager struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of RancherManager
	// +required
	Spec RancherManagerSpec `json:"spec"`

	// status defines the observed state of RancherManager
	// +optional
	Status RancherManagerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// RancherManagerList contains a list of RancherManager
type RancherManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []RancherManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &RancherManager{}, &RancherManagerList{})
		return nil
	})
}

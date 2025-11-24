/*
Copyright 2025.

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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ManagedArgoCDProjectSpec defines the desired state of ManagedArgoCDProject
type ManagedArgoCDProjectSpec struct {
	// ProjectName is the name of the ArgoCD AppProject to create/manage
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ProjectName string `json:"projectName"`

	// Repositories is the list of repository URLs that applications in this project can deploy from
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Repositories []string `json:"repositories"`

	// Destinations is the list of destination clusters and namespaces
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Destinations []ApplicationDestination `json:"destinations"`

	// Template specifies which project template to use (standard, privileged, restricted)
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=standard
	// +kubebuilder:validation:Enum=standard;privileged;restricted
	Template string `json:"template,omitempty"`

	// Description provides a human-readable description of the project
	// +kubebuilder:validation:Optional
	Description string `json:"description,omitempty"`
}

// ApplicationDestination defines a destination cluster and namespace
type ApplicationDestination struct {
	// Server is the URL of the target cluster
	// +kubebuilder:validation:Required
	Server string `json:"server"`

	// Namespace is the target namespace
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`

	// Name is an optional friendly name for the destination
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`
}

// ManagedArgoCDProjectStatus defines the observed state of ManagedArgoCDProject
type ManagedArgoCDProjectStatus struct {
	// Conditions represent the latest available observations of the project's state
	// +kubebuilder:validation:Optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ProjectName is the name of the created ArgoCD AppProject
	// +kubebuilder:validation:Optional
	ProjectName string `json:"projectName,omitempty"`

	// Phase represents the current phase of the project (Pending, Ready, Failed)
	// +kubebuilder:validation:Optional
	Phase string `json:"phase,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	// +kubebuilder:validation:Optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastSyncTime is the timestamp of the last successful sync
	// +kubebuilder:validation:Optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// RenderedYAML contains the rendered AppProject manifest (useful for GitOps export)
	// +kubebuilder:validation:Optional
	RenderedYAML string `json:"renderedYAML,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=macp;macproject
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectName`
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.template`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ManagedArgoCDProject is the Schema for the managedargoCDprojects API
type ManagedArgoCDProject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedArgoCDProjectSpec   `json:"spec,omitempty"`
	Status ManagedArgoCDProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedArgoCDProjectList contains a list of ManagedArgoCDProject
type ManagedArgoCDProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedArgoCDProject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedArgoCDProject{}, &ManagedArgoCDProjectList{})
}

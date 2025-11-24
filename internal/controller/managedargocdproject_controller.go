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

package controller

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	argoCDv1alpha1 "github.com/dudick123/argocd-project-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Finalizer name
	finalizerName = "argocd.platform.io/finalizer"

	// Condition types
	conditionTypeReady = "Ready"

	// Phase constants
	phasePending = "Pending"
	phaseReady   = "Ready"
	phaseFailed  = "Failed"

	// Annotation for exporting to Git
	annotationExportPath = "argocd.platform.io/export-path"
)

// ManagedArgoCDProjectReconciler reconciles a ManagedArgoCDProject object
type ManagedArgoCDProjectReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=argocd.platform.io,resources=managedargoCDprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=argocd.platform.io,resources=managedargoCDprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=argocd.platform.io,resources=managedargoCDprojects/finalizers,verbs=update
// +kubebuilder:rbac:groups=argoproj.io,resources=appprojects,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *ManagedArgoCDProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the ManagedArgoCDProject instance
	managedProject := &argoCDv1alpha1.ManagedArgoCDProject{}
	err := r.Get(ctx, req.NamespacedName, managedProject)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("ManagedArgoCDProject resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get ManagedArgoCDProject")
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer
	if managedProject.ObjectMeta.DeletionTimestamp.IsZero() {
		// Object is not being deleted, add finalizer if needed
		if !controllerutil.ContainsFinalizer(managedProject, finalizerName) {
			controllerutil.AddFinalizer(managedProject, finalizerName)
			if err := r.Update(ctx, managedProject); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// Object is being deleted
		if controllerutil.ContainsFinalizer(managedProject, finalizerName) {
			// Perform cleanup (delete ArgoCD AppProject)
			if err := r.deleteArgoCDProject(ctx, managedProject); err != nil {
				log.Error(err, "Failed to delete ArgoCD AppProject")
				return ctrl.Result{}, err
			}

			// Remove finalizer
			controllerutil.RemoveFinalizer(managedProject, finalizerName)
			if err := r.Update(ctx, managedProject); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Load the appropriate template
	template, err := r.loadTemplate(managedProject.Spec.Template)
	if err != nil {
		log.Error(err, "Failed to load template", "template", managedProject.Spec.Template)
		r.updateStatus(ctx, managedProject, phaseFailed, fmt.Sprintf("Failed to load template: %v", err), "")
		return ctrl.Result{}, err
	}

	// Render the ArgoCD AppProject from template
	appProject, err := r.renderArgoCDProject(managedProject, template)
	if err != nil {
		log.Error(err, "Failed to render ArgoCD AppProject")
		r.updateStatus(ctx, managedProject, phaseFailed, fmt.Sprintf("Failed to render project: %v", err), "")
		return ctrl.Result{}, err
	}

	// Convert to YAML for status (useful for GitOps export)
	renderedYAML, err := yaml.Marshal(appProject.Object)
	if err != nil {
		log.Error(err, "Failed to marshal AppProject to YAML")
		renderedYAML = []byte("# Error marshaling YAML")
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(managedProject, appProject, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference")
		return ctrl.Result{}, err
	}

	// Create or update the ArgoCD AppProject
	foundProject := &unstructured.Unstructured{}
	foundProject.SetGroupVersionKind(appProject.GroupVersionKind())

	err = r.Get(ctx, types.NamespacedName{
		Name:      managedProject.Spec.ProjectName,
		Namespace: managedProject.Namespace,
	}, foundProject)

	if err != nil && errors.IsNotFound(err) {
		// Create new AppProject
		log.Info("Creating ArgoCD AppProject", "name", managedProject.Spec.ProjectName)
		err = r.Create(ctx, appProject)
		if err != nil {
			log.Error(err, "Failed to create ArgoCD AppProject")
			r.updateStatus(ctx, managedProject, phaseFailed, fmt.Sprintf("Failed to create project: %v", err), "")
			return ctrl.Result{}, err
		}
		r.updateStatus(ctx, managedProject, phaseReady, "ArgoCD AppProject created successfully", string(renderedYAML))
		return ctrl.Result{}, nil
	} else if err != nil {
		log.Error(err, "Failed to get ArgoCD AppProject")
		return ctrl.Result{}, err
	}

	// Update existing AppProject
	log.Info("Updating ArgoCD AppProject", "name", managedProject.Spec.ProjectName)
	appProject.SetResourceVersion(foundProject.GetResourceVersion())
	err = r.Update(ctx, appProject)
	if err != nil {
		log.Error(err, "Failed to update ArgoCD AppProject")
		r.updateStatus(ctx, managedProject, phaseFailed, fmt.Sprintf("Failed to update project: %v", err), "")
		return ctrl.Result{}, err
	}

	r.updateStatus(ctx, managedProject, phaseReady, "ArgoCD AppProject updated successfully", string(renderedYAML))
	return ctrl.Result{}, nil
}

// loadTemplate loads the appropriate project template based on the template name
func (r *ManagedArgoCDProjectReconciler) loadTemplate(templateName string) (map[string]interface{}, error) {
	templates := map[string]map[string]interface{}{
		"standard": {
			"clusterResourceWhitelist": []map[string]string{
				{"group": "*", "kind": "Namespace"},
			},
			"namespaceResourceWhitelist": []map[string]string{
				{"group": "apps", "kind": "*"},
				{"group": "", "kind": "*"},
				{"group": "batch", "kind": "*"},
				{"group": "networking.k8s.io", "kind": "*"},
				{"group": "autoscaling", "kind": "*"},
			},
			"sourceNamespaces": []string{},
			"roles": []map[string]interface{}{
				{
					"name": "read-only",
					"description": "Read-only access to applications",
					"policies": []string{
						"p, proj:{{PROJECT}}:read-only, applications, get, {{PROJECT}}/*, allow",
					},
				},
				{
					"name": "developer",
					"description": "Developer access with sync capabilities",
					"policies": []string{
						"p, proj:{{PROJECT}}:developer, applications, get, {{PROJECT}}/*, allow",
						"p, proj:{{PROJECT}}:developer, applications, sync, {{PROJECT}}/*, allow",
					},
				},
				{
					"name": "admin",
					"description": "Full administrative access",
					"policies": []string{
						"p, proj:{{PROJECT}}:admin, applications, *, {{PROJECT}}/*, allow",
					},
				},
			},
			"orphanedResources": map[string]interface{}{
				"warn": true,
			},
		},
		"privileged": {
			"clusterResourceWhitelist": []map[string]string{
				{"group": "*", "kind": "*"},
			},
			"namespaceResourceWhitelist": []map[string]string{
				{"group": "*", "kind": "*"},
			},
			"sourceNamespaces": []string{},
			"roles": []map[string]interface{}{
				{
					"name": "platform-admin",
					"description": "Platform team full access",
					"policies": []string{
						"p, proj:{{PROJECT}}:platform-admin, applications, *, {{PROJECT}}/*, allow",
						"p, proj:{{PROJECT}}:platform-admin, clusters, *, *, allow",
						"p, proj:{{PROJECT}}:platform-admin, repositories, *, *, allow",
					},
				},
			},
			"orphanedResources": map[string]interface{}{
				"warn": false,
			},
		},
		"restricted": {
			"clusterResourceWhitelist": []map[string]string{},
			"namespaceResourceWhitelist": []map[string]string{
				{"group": "apps", "kind": "Deployment"},
				{"group": "apps", "kind": "StatefulSet"},
				{"group": "apps", "kind": "DaemonSet"},
				{"group": "", "kind": "Service"},
				{"group": "", "kind": "ConfigMap"},
				{"group": "", "kind": "Secret"},
				{"group": "networking.k8s.io", "kind": "Ingress"},
				{"group": "networking.k8s.io", "kind": "NetworkPolicy"},
			},
			"sourceNamespaces": []string{},
			"roles": []map[string]interface{}{
				{
					"name": "read-only",
					"description": "Read-only access only",
					"policies": []string{
						"p, proj:{{PROJECT}}:read-only, applications, get, {{PROJECT}}/*, allow",
					},
				},
			},
			"orphanedResources": map[string]interface{}{
				"warn": true,
			},
		},
	}

	if templateName == "" {
		templateName = "standard"
	}

	template, ok := templates[templateName]
	if !ok {
		return nil, fmt.Errorf("template %s not found", templateName)
	}

	return template, nil
}

// renderArgoCDProject creates an ArgoCD AppProject from the template and user spec
func (r *ManagedArgoCDProjectReconciler) renderArgoCDProject(
	managedProject *argoCDv1alpha1.ManagedArgoCDProject,
	template map[string]interface{},
) (*unstructured.Unstructured, error) {

	// Build destinations array
	destinations := []map[string]string{}
	for _, dest := range managedProject.Spec.Destinations {
		destMap := map[string]string{
			"server":    dest.Server,
			"namespace": dest.Namespace,
		}
		if dest.Name != "" {
			destMap["name"] = dest.Name
		}
		destinations = append(destinations, destMap)
	}

	// Process roles to replace placeholders
	roles := template["roles"].([]map[string]interface{})
	processedRoles := []map[string]interface{}{}
	for _, role := range roles {
		processedRole := map[string]interface{}{
			"name": role["name"],
		}

		if desc, ok := role["description"]; ok {
			processedRole["description"] = desc
		}

		policies := role["policies"].([]string)
		processedPolicies := []string{}
		for _, policy := range policies {
			processedPolicies = append(processedPolicies,
				strings.ReplaceAll(policy, "{{PROJECT}}", managedProject.Spec.ProjectName))
		}
		processedRole["policies"] = processedPolicies
		processedRoles = append(processedRoles, processedRole)
	}

	// Create the AppProject spec
	spec := map[string]interface{}{
		"sourceRepos":                managedProject.Spec.Repositories,
		"destinations":               destinations,
		"clusterResourceWhitelist":   template["clusterResourceWhitelist"],
		"namespaceResourceWhitelist": template["namespaceResourceWhitelist"],
		"sourceNamespaces":           template["sourceNamespaces"],
		"roles":                      processedRoles,
		"orphanedResources":          template["orphanedResources"],
	}

	if managedProject.Spec.Description != "" {
		spec["description"] = managedProject.Spec.Description
	}

	// Create unstructured AppProject
	appProject := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "AppProject",
			"metadata": map[string]interface{}{
				"name":      managedProject.Spec.ProjectName,
				"namespace": managedProject.Namespace,
				"labels": map[string]interface{}{
					"managed-by":               "argocd-project-operator",
					"argocd.platform.io/template": managedProject.Spec.Template,
				},
			},
			"spec": spec,
		},
	}

	return appProject, nil
}

// deleteArgoCDProject deletes the ArgoCD AppProject
func (r *ManagedArgoCDProjectReconciler) deleteArgoCDProject(
	ctx context.Context,
	managedProject *argoCDv1alpha1.ManagedArgoCDProject,
) error {
	appProject := &unstructured.Unstructured{}
	appProject.SetGroupVersionKind(ctrl.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "AppProject",
	})

	err := r.Get(ctx, types.NamespacedName{
		Name:      managedProject.Spec.ProjectName,
		Namespace: managedProject.Namespace,
	}, appProject)

	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return r.Delete(ctx, appProject)
}

// updateStatus updates the status of the ManagedArgoCDProject
func (r *ManagedArgoCDProjectReconciler) updateStatus(
	ctx context.Context,
	managedProject *argoCDv1alpha1.ManagedArgoCDProject,
	phase string,
	message string,
	renderedYAML string,
) {
	managedProject.Status.Phase = phase
	managedProject.Status.ProjectName = managedProject.Spec.ProjectName
	managedProject.Status.ObservedGeneration = managedProject.Generation
	managedProject.Status.RenderedYAML = renderedYAML

	if phase == phaseReady {
		now := metav1.Now()
		managedProject.Status.LastSyncTime = &now
	}

	condition := metav1.Condition{
		Type:               conditionTypeReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: managedProject.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             phase,
		Message:            message,
	}

	if phase == phaseFailed {
		condition.Status = metav1.ConditionFalse
	}

	// Update or append condition
	found := false
	for i, c := range managedProject.Status.Conditions {
		if c.Type == conditionTypeReady {
			managedProject.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		managedProject.Status.Conditions = append(managedProject.Status.Conditions, condition)
	}

	if err := r.Status().Update(ctx, managedProject); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update status")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedArgoCDProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&argoCDv1alpha1.ManagedArgoCDProject{}).
		Complete(r)
}

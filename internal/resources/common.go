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

package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

// BuildNamespace creates a Namespace resource
func BuildNamespace(name string, deployment *carbitev1alpha1.CarbideDeployment) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "carbide",
				"app.kubernetes.io/managed-by": "carbide-operator",
				"app.kubernetes.io/instance":   deployment.Name,
			},
		},
	}
}

// DefaultLabels returns the default labels for BMM resources
func DefaultLabels(component string, deployment *carbitev1alpha1.CarbideDeployment) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "carbide",
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/managed-by": "carbide-operator",
		"app.kubernetes.io/instance":   deployment.Name,
	}
}

// GetImagePullPolicy returns the image pull policy from spec or default
func GetImagePullPolicy(deployment *carbitev1alpha1.CarbideDeployment) corev1.PullPolicy {
	if deployment.Spec.Images != nil && deployment.Spec.Images.PullPolicy != "" {
		return deployment.Spec.Images.PullPolicy
	}
	return corev1.PullIfNotPresent
}

// GetImageRegistry returns the image registry from spec or default
func GetImageRegistry(deployment *carbitev1alpha1.CarbideDeployment) string {
	if deployment.Spec.Images != nil && deployment.Spec.Images.Registry != "" {
		return deployment.Spec.Images.Registry
	}
	return "ghcr.io/nvidia"
}

// DefaultLabelsUnstructured returns labels as map[string]interface{} for use in
// unstructured objects. The fake client's deep copy requires all nested map values
// to be interface{} types, not concrete maps.
func DefaultLabelsUnstructured(component string, deployment *carbitev1alpha1.CarbideDeployment) map[string]interface{} {
	labels := DefaultLabels(component, deployment)
	result := make(map[string]interface{}, len(labels))
	for k, v := range labels {
		result[k] = v
	}
	return result
}

// GetStorageClass returns the storage class to use for a component
func GetStorageClass(deployment *carbitev1alpha1.CarbideDeployment, storageSpec *carbitev1alpha1.StorageSpec) string {
	// Component-specific storage class takes precedence
	if storageSpec != nil && storageSpec.StorageClass != "" {
		return storageSpec.StorageClass
	}
	// Then infrastructure-level storage class
	if deployment.Spec.Infrastructure != nil && deployment.Spec.Infrastructure.StorageClass != "" {
		return deployment.Spec.Infrastructure.StorageClass
	}
	// Empty string means use cluster default
	return ""
}

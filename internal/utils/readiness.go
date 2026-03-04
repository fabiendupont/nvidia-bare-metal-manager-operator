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

package utils

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateStorageClass checks if a StorageClass exists
func ValidateStorageClass(ctx context.Context, c client.Client, name string) error {
	if name == "" {
		return nil // Empty name means use default
	}

	var sc storagev1.StorageClass
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &sc); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("StorageClass %q not found", name)
		}
		return fmt.Errorf("failed to get StorageClass %q: %w", name, err)
	}
	return nil
}

// GetDefaultStorageClass retrieves the default StorageClass
func GetDefaultStorageClass(ctx context.Context, c client.Client) (string, error) {
	var scList storagev1.StorageClassList
	if err := c.List(ctx, &scList); err != nil {
		return "", fmt.Errorf("failed to list StorageClasses: %w", err)
	}

	for _, sc := range scList.Items {
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			return sc.Name, nil
		}
	}

	return "", fmt.Errorf("no default StorageClass found")
}

// IsDeploymentReady checks if a Deployment is ready
func IsDeploymentReady(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var deployment appsv1.Deployment
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &deployment); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check if deployment has desired replicas available
	if deployment.Spec.Replicas != nil {
		if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas &&
			deployment.Status.UpdatedReplicas == *deployment.Spec.Replicas &&
			deployment.Status.ObservedGeneration >= deployment.Generation {
			return true, nil
		}
	}

	return false, nil
}

// IsDaemonSetReady checks if a DaemonSet is ready
func IsDaemonSetReady(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var ds appsv1.DaemonSet
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &ds); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check if DaemonSet has all pods ready
	if ds.Status.DesiredNumberScheduled > 0 &&
		ds.Status.NumberReady == ds.Status.DesiredNumberScheduled &&
		ds.Status.UpdatedNumberScheduled == ds.Status.DesiredNumberScheduled &&
		ds.Status.ObservedGeneration >= ds.Generation {
		return true, nil
	}

	return false, nil
}

// IsStatefulSetReady checks if a StatefulSet is ready
func IsStatefulSetReady(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var sts appsv1.StatefulSet
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &sts); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check if StatefulSet has desired replicas ready
	if sts.Spec.Replicas != nil {
		if sts.Status.ReadyReplicas == *sts.Spec.Replicas &&
			sts.Status.CurrentReplicas == *sts.Spec.Replicas &&
			sts.Status.UpdatedReplicas == *sts.Spec.Replicas &&
			sts.Status.ObservedGeneration >= sts.Generation {
			return true, nil
		}
	}

	return false, nil
}

// IsJobComplete checks if a Job has completed successfully
func IsJobComplete(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var job batchv1.Job
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &job); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check for completion
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true, nil
		}
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return false, fmt.Errorf("job %s/%s failed", namespace, name)
		}
	}

	return false, nil
}

// IsPodReady checks if a specific Pod is ready
func IsPodReady(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &pod); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check if pod is running and all containers are ready
	if pod.Status.Phase == corev1.PodRunning {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
	}

	return false, nil
}

// ArePodsReady checks if all pods matching a label selector are ready
func ArePodsReady(ctx context.Context, c client.Client, namespace string, labels map[string]string) (bool, int, error) {
	var podList corev1.PodList
	if err := c.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return false, 0, err
	}

	if len(podList.Items) == 0 {
		return false, 0, nil
	}

	readyCount := 0
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					readyCount++
					break
				}
			}
		}
	}

	return readyCount == len(podList.Items), readyCount, nil
}

// IsSecretAvailable checks if a Secret exists
func IsSecretAvailable(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsConfigMapAvailable checks if a ConfigMap exists
func IsConfigMapAvailable(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cm); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsServiceAvailable checks if a Service exists
func IsServiceAvailable(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var svc corev1.Service
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &svc); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsPVCBound checks if a PersistentVolumeClaim is bound
func IsPVCBound(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	var pvc corev1.PersistentVolumeClaim
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &pvc); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return pvc.Status.Phase == corev1.ClaimBound, nil
}

// IsPostgreSQLReady checks if the PostgresCluster CR is ready (from postgres-operator)
func IsPostgreSQLReady(ctx context.Context, c client.Client, namespace, name string) (bool, error) {
	pgCluster := &unstructured.Unstructured{}
	pgCluster.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "postgres-operator.crunchydata.com",
		Version: "v1beta1",
		Kind:    "PostgresCluster",
	})

	if err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, pgCluster); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check status.instances for readiness
	status, found, err := unstructured.NestedMap(pgCluster.Object, "status")
	if err != nil || !found {
		return false, err
	}

	// Check for instances ready
	instances, found, err := unstructured.NestedSlice(status, "instances")
	if err != nil || !found || len(instances) == 0 {
		return false, nil
	}

	// Check first instance status
	instance, ok := instances[0].(map[string]interface{})
	if !ok {
		return false, nil
	}

	readyReplicas, found, err := unstructured.NestedInt64(instance, "readyReplicas")
	if err != nil || !found {
		return false, nil
	}

	replicas, found, err := unstructured.NestedInt64(instance, "replicas")
	if err != nil || !found {
		return false, nil
	}

	// All replicas must be ready
	if readyReplicas > 0 && readyReplicas == replicas {
		return true, nil
	}

	return false, nil
}

// IsTemporalHelmReady checks if Temporal deployed via Helm is ready
// by looking for the temporal-frontend Deployment.
func IsTemporalHelmReady(ctx context.Context, c client.Client, namespace string) (bool, error) {
	return IsDeploymentReady(ctx, c, namespace, "temporal-frontend")
}

// IsVaultReady checks if Vault StatefulSet is ready and the unseal secret exists.
func IsVaultReady(ctx context.Context, c client.Client, namespace string) (bool, error) {
	// Check if vault-0 pod is ready
	stsReady, err := IsStatefulSetReady(ctx, c, namespace, "vault")
	if err != nil || !stsReady {
		return false, err
	}

	// Check if unseal secret exists with real values
	secretReady, err := IsSecretAvailable(ctx, c, namespace, "vault-unseal-secret")
	if err != nil {
		return false, err
	}

	return stsReady && secretReady, nil
}

// IsKeycloakReady checks if the Keycloak CR is ready (from RHBK operator)
func IsKeycloakReady(ctx context.Context, c client.Client, namespace string) (bool, error) {
	keycloak := &unstructured.Unstructured{}
	keycloak.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "k8s.keycloak.org",
		Version: "v2alpha1",
		Kind:    "Keycloak",
	})

	if err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      "carbide-keycloak",
	}, keycloak); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	// Check status.conditions for Ready=True
	status, found, err := unstructured.NestedMap(keycloak.Object, "status")
	if err != nil || !found {
		return false, nil
	}

	conditions, found, err := unstructured.NestedSlice(status, "conditions")
	if err != nil || !found {
		return false, nil
	}

	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		if condType == "Ready" && condStatus == "True" {
			return true, nil
		}
	}

	return false, nil
}

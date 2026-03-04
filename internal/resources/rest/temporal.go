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

package rest

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

const (
	TemporalNamespace       = "temporal"
	TemporalReleaseName     = "temporal"
	TemporalHelmJobName     = "temporal-helm-install"
	TemporalValuesConfigMap = "temporal-helm-values"
)

// BuildTemporalHelmValuesConfigMap creates a ConfigMap with Temporal Helm chart values.
func BuildTemporalHelmValuesConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace, pgHost string, pgPort int32, pgSecretName string) *corev1.ConfigMap {
	temporalConfig := deployment.Spec.Rest.Temporal

	_ = temporalConfig.ChartVersion // used in BuildTemporalHelmJob

	valuesYAML := fmt.Sprintf(`server:
  config:
    persistence:
      default:
        driver: sql
        sql:
          driver: postgres12
          host: %s
          port: %d
          database: temporal
          user: temporal
          existingSecret: %s
          maxConns: 20
          maxIdleConns: 20
          maxConnLifetime: 1h
      visibility:
        driver: sql
        sql:
          driver: postgres12
          host: %s
          port: %d
          database: temporal_visibility
          user: temporal
          existingSecret: %s
          maxConns: 20
          maxIdleConns: 20
          maxConnLifetime: 1h
  replicaCount: %d
cassandra:
  enabled: false
mysql:
  enabled: false
postgresql:
  enabled: false
elasticsearch:
  enabled: false
schema:
  setup:
    enabled: true
  update:
    enabled: true
admintools:
  enabled: true
web:
  enabled: false
`, pgHost, pgPort, pgSecretName,
		pgHost, pgPort, pgSecretName,
		temporalConfig.Replicas)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TemporalValuesConfigMap,
			Namespace: namespace,
			Labels:    resources.DefaultLabels("temporal-helm-values", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: map[string]string{
			"values.yaml": valuesYAML,
		},
	}
}

// BuildTemporalHelmJob creates a Job that runs helm install for Temporal.
func BuildTemporalHelmJob(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *batchv1.Job {
	labels := resources.DefaultLabels("temporal-helm", deployment)
	backoffLimit := int32(5)
	ttlAfterFinished := int32(86400)

	temporalNs := deployment.Spec.Rest.Temporal.Namespace
	if temporalNs == "" {
		temporalNs = TemporalNamespace
	}

	chartVersion := deployment.Spec.Rest.Temporal.ChartVersion
	if chartVersion == "" {
		chartVersion = "0.73.1"
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TemporalHelmJobName,
			Namespace: namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: "temporal-helm-installer",
					Containers: []corev1.Container{
						{
							Name:            "helm",
							Image:           "alpine/helm:latest",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/sh"},
							Args: []string{
								"-c",
								fmt.Sprintf(`helm repo add temporal https://go.temporal.io/helm-charts && \
helm repo update && \
helm upgrade --install %s temporal/temporal \
  --version %s \
  -n %s --create-namespace \
  -f /values/values.yaml \
  --wait --timeout 15m`,
									TemporalReleaseName, chartVersion, temporalNs),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "values",
									MountPath: "/values",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "values",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: TemporalValuesConfigMap,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// GetTemporalFrontendURL returns the Temporal frontend service URL
func GetTemporalFrontendURL(namespace string) string {
	if namespace == "" {
		namespace = TemporalNamespace
	}
	return fmt.Sprintf("%s-frontend.%s.svc:7233", TemporalReleaseName, namespace)
}

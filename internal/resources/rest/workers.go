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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	CloudWorkerName = "carbide-rest-cloud-worker"
	SiteWorkerName  = "carbide-rest-site-worker"
	WorkflowCMName  = "carbide-rest-workflow-config"
)

// BuildWorkflowConfigMap creates the workflow config ConfigMap (shared by cloud-worker and site-worker).
func BuildWorkflowConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ConfigMap {
	temporalNs := deployment.Spec.Rest.Temporal.Namespace
	if temporalNs == "" {
		temporalNs = "temporal"
	}
	_ = temporalNs // used in configYAML template below

	// YAML config matching SNO reference
	configYAML := fmt.Sprintf(`temporal:
  host: temporal-frontend
  port: 7233
  serverName: temporal-frontend
  namespace: cloud
  queue: cloud
  tls:
    enabled: true
    certPath: %s/tls.crt
    keyPath: %s/tls.key
    caPath: %s/ca.crt
  encryptionKeyPath: /var/secrets/temporal-encryption/encryption-key
db:
  tls:
    enabled: true
    caPath: %s/ca.crt
    certPath: %s/tls.crt
    keyPath: %s/tls.key
`, tls.CertDir, tls.CertDir, tls.CertDir,
		tls.CertDir, tls.CertDir, tls.CertDir)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      WorkflowCMName,
			Namespace: namespace,
			Labels:    resources.DefaultLabels("workflow-config", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: map[string]string{
			"config.yaml": configYAML,
		},
	}
}

// BuildCloudWorkerDeployment creates the cloud-worker Deployment.
func BuildCloudWorkerDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	return buildWorkerDeployment(deployment, namespace, CloudWorkerName, "cloud")
}

// BuildSiteWorkerDeployment creates the site-worker Deployment.
func BuildSiteWorkerDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	return buildWorkerDeployment(deployment, namespace, SiteWorkerName, "site")
}

func buildWorkerDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace, name, temporalNamespace string) *appsv1.Deployment {
	labels := resources.DefaultLabels(name, deployment)
	labels["app"] = name

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-workflow:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.Workflow != "" {
		imageName = deployment.Spec.Images.Workflow
	}

	replicas := int32(1)
	falseVal := false

	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/app/config.yaml",
			SubPath:   "config.yaml",
			ReadOnly:  true,
		},
		{
			Name:      "temporal-encryption-key",
			MountPath: "/var/secrets/temporal-encryption",
			ReadOnly:  true,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: WorkflowCMName,
					},
				},
			},
		},
		{
			Name: "temporal-encryption-key",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "temporal-encryption-key",
				},
			},
		},
	}

	env := []corev1.EnvVar{
		{Name: "CONFIG_FILE_PATH", Value: "/app/config.yaml"},
		{Name: "TEMPORAL_NAMESPACE", Value: temporalNamespace},
		{Name: "TEMPORAL_QUEUE", Value: temporalNamespace},
		{
			Name: "DB_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "carbide-postgres-pguser-forge",
					},
					Key: "password",
				},
			},
		},
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: name,
		Containers: []corev1.Container{
			{
				Name:            name,
				Image:           imageName,
				ImagePullPolicy: resources.GetImagePullPolicy(deployment),
				Env:             env,
				VolumeMounts:    volumeMounts,
				Resources:       res,
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &falseVal,
					RunAsNonRoot:             boolPtr(true),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
				},
			},
		},
		Volumes: volumes,
	}

	tls.InjectTLS(&podSpec, deployment)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: podSpec,
			},
		},
	}
}

// BuildWorkerServiceAccount creates a ServiceAccount for a worker.
func BuildWorkerServiceAccount(name, namespace string, deployment *carbitev1alpha1.CarbideDeployment) *corev1.ServiceAccount {
	automount := true
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    resources.DefaultLabels(name, deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		AutomountServiceAccountToken: &automount,
	}
}

func boolPtr(b bool) *bool {
	return &b
}

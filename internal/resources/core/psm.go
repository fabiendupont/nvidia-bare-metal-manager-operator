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

package core

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/infrastructure"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	PSMName = "carbide-psm"
)

// BuildPSMDeployment creates the PSM Deployment.
func BuildPSMDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	psmConfig := deployment.Spec.Core.PSM
	if psmConfig == nil {
		return nil
	}

	replicas := psmConfig.Replicas
	if replicas == 0 {
		replicas = 1
	}

	port := psmConfig.Port
	if port == 0 {
		port = 50051
	}

	labels := resources.DefaultLabels("psm", deployment)
	labels["app"] = PSMName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-psm:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.PSM != "" {
		imageName = deployment.Spec.Images.PSM
	}

	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	}
	if psmConfig.Resources != nil {
		res = *psmConfig.Resources
	}

	pgSecretName := infrastructure.ResolveUserSecret(deployment, "psm")

	env := []corev1.EnvVar{
		{Name: "DB_ADDR", ValueFrom: secretKeyRef(pgSecretName, "host")},
		{Name: "DB_PORT", ValueFrom: secretKeyRef(pgSecretName, "port")},
		{Name: "DB_USER", ValueFrom: secretKeyRef(pgSecretName, "user")},
		{Name: "DB_PASSWORD", ValueFrom: secretKeyRef(pgSecretName, "password")},
		{Name: "DB_DATABASE", ValueFrom: secretKeyRef(pgSecretName, "dbname")},
		{Name: "DB_CERT_PATH", Value: "/var/run/secrets/db/ca.crt"},
		{Name: "CERTDIR", Value: tls.CertDir},
	}

	// Add Vault env vars
	if deployment.Spec.Core.Vault != nil {
		if deployment.Spec.Core.Vault.Mode == carbitev1alpha1.ExternalMode && deployment.Spec.Core.Vault.Address != "" {
			env = append(env, corev1.EnvVar{Name: "VAULT_ADDR", Value: deployment.Spec.Core.Vault.Address})
			if deployment.Spec.Core.Vault.TokenSecretRef != nil {
				key := deployment.Spec.Core.Vault.TokenSecretRef.Key
				if key == "" {
					key = "token"
				}
				env = append(env, corev1.EnvVar{
					Name:      "VAULT_TOKEN",
					ValueFrom: secretKeyRef(deployment.Spec.Core.Vault.TokenSecretRef.Name, key),
				})
			}
		} else if deployment.Spec.Core.Vault.Mode == carbitev1alpha1.ManagedMode {
			env = append(env,
				corev1.EnvVar{Name: "VAULT_ADDR", Value: fmt.Sprintf("http://vault.%s.svc:8200", namespace)},
				corev1.EnvVar{Name: "VAULT_TOKEN", ValueFrom: secretKeyRef("vault-unseal-secret", "root-token")},
			)
		}
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "db-certs", MountPath: "/var/run/secrets/db", ReadOnly: true},
	}

	volumes := []corev1.Volume{
		{
			Name: "db-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: pgSecretName,
					Items:      []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
					Optional:   boolPtr(true),
				},
			},
		},
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: PSMName,
		Volumes:            volumes,
		Containers: []corev1.Container{
			{
				Name:            "carbide-psm",
				Image:           imageName,
				ImagePullPolicy: resources.GetImagePullPolicy(deployment),
				Command:         []string{"/app/psm"},
				Args:            []string{"serve", "--port", fmt.Sprintf("%d", port), "--datastore", "Persistent"},
				Ports: []corev1.ContainerPort{
					{
						Name:          "grpc",
						ContainerPort: port,
						Protocol:      corev1.ProtocolTCP,
					},
				},
				Env:          env,
				VolumeMounts: volumeMounts,
				Resources:    res,
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt32(port),
						},
					},
					InitialDelaySeconds: 30,
					PeriodSeconds:       10,
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt32(port),
						},
					},
					InitialDelaySeconds: 5,
					PeriodSeconds:       5,
				},
			},
		},
	}

	tls.InjectTLS(&podSpec, deployment)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PSMName,
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
					"app": PSMName,
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

// BuildPSMService creates the PSM Service.
func BuildPSMService(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.Service {
	port := int32(50051)
	if deployment.Spec.Core.PSM != nil && deployment.Spec.Core.PSM.Port > 0 {
		port = deployment.Spec.Core.PSM.Port
	}

	labels := resources.DefaultLabels("psm-service", deployment)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PSMName,
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
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": PSMName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

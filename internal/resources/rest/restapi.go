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
	"k8s.io/apimachinery/pkg/util/intstr"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/spiffe"
)

const (
	RestAPIName = "carbide-rest-api"
)

// BuildRestAPIConfigMap creates the REST API ConfigMap with YAML config.
func BuildRestAPIConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace, temporalEndpoint, keycloakURL string) *corev1.ConfigMap {
	keycloakConfig := deployment.Spec.Rest.Keycloak
	coreNamespace := deployment.Spec.Core.Namespace
	if coreNamespace == "" {
		coreNamespace = namespace
	}

	// YAML config matching SNO reference
	configYAML := fmt.Sprintf(`temporal:
  host: temporal-frontend
  port: 7233
  serverName: temporal-frontend
  namespace: cloud
  queue: cloud
  tls:
    enabled: true
    caPath: %s/ca.crt
    certPath: %s/tls.crt
    keyPath: %s/tls.key
db:
  tls:
    enabled: true
    caPath: %s/ca.crt
    certPath: %s/tls.crt
    keyPath: %s/tls.key
auth:
  - name: keycloak
    origin: 2
    url: %s/realms/%s/protocol/openid-connect/certs
siteManager:
  endpoint: http://site-manager.%s.svc:8080
`,
		spiffe.CertDir, spiffe.CertDir, spiffe.CertDir,
		spiffe.CertDir, spiffe.CertDir, spiffe.CertDir,
		keycloakURL, keycloakConfig.Realm,
		namespace,
	)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", RestAPIName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("rest-api-config", deployment),
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

// BuildRestAPIDeployment creates the REST API Deployment.
func BuildRestAPIDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	restAPIConfig := deployment.Spec.Rest.RestAPI

	replicas := restAPIConfig.Replicas
	if replicas == 0 {
		replicas = 1
	}

	port := restAPIConfig.Port
	if port == 0 {
		port = 8080
	}

	labels := resources.DefaultLabels("rest-api", deployment)
	labels["app"] = RestAPIName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-rest:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.RestAPI != "" {
		imageName = deployment.Spec.Images.RestAPI
	}

	// Resource defaults matching SNO manifests
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
	if restAPIConfig.Resources != nil {
		res = *restAPIConfig.Resources
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/app/config.yaml",
			SubPath:   "config.yaml",
			ReadOnly:  true,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-config", RestAPIName),
					},
				},
			},
		},
	}

	if spiffe.IsEnabled(deployment) {
		volumeMounts = append(volumeMounts, spiffe.SpiffeCertVolumeMount())
	}

	env := []corev1.EnvVar{
		{Name: "CONFIG_FILE_PATH", Value: "/app/config.yaml"},
		{
			Name: "DB_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-secret", RestAPIName),
					},
					Key: "DB_PASSWORD",
				},
			},
		},
	}

	migrateContainer := corev1.Container{
		Name:            "db-migrations",
		Image:           imageName,
		ImagePullPolicy: resources.GetImagePullPolicy(deployment),
		Command:         []string{"/usr/local/bin/carbide-rest"},
		Args:            []string{"migrate"},
		Env: []corev1.EnvVar{
			{
				Name: "DB_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: fmt.Sprintf("%s-secret", RestAPIName),
						},
						Key: "DB_PASSWORD",
					},
				},
			},
		},
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: RestAPIName,
		InitContainers:     []corev1.Container{migrateContainer},
		Containers: []corev1.Container{
			{
				Name:            "rest-api",
				Image:           imageName,
				ImagePullPolicy: resources.GetImagePullPolicy(deployment),
				Ports: []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: port,
						Protocol:      corev1.ProtocolTCP,
					},
				},
				Env:          env,
				VolumeMounts: volumeMounts,
				Resources:    res,
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/healthz",
							Port: intstr.FromInt32(port),
						},
					},
					InitialDelaySeconds: 30,
					PeriodSeconds:       10,
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/readyz",
							Port: intstr.FromInt32(port),
						},
					},
					InitialDelaySeconds: 5,
					PeriodSeconds:       5,
				},
			},
		},
		Volumes: volumes,
	}

	spiffe.InjectSpiffe(&podSpec, deployment)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RestAPIName,
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
					"app": RestAPIName,
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

// BuildRestAPIService creates the REST API Service.
func BuildRestAPIService(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.Service {
	restAPIConfig := deployment.Spec.Rest.RestAPI

	port := restAPIConfig.Port
	if port == 0 {
		port = 8080
	}

	labels := resources.DefaultLabels("rest-api-service", deployment)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RestAPIName,
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
				"app": RestAPIName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	if restAPIConfig.NodePort > 0 {
		svc.Spec.Type = corev1.ServiceTypeNodePort
		svc.Spec.Ports[0].NodePort = restAPIConfig.NodePort
	}

	return svc
}

// BuildRestAPISecret creates the REST API Secret.
func BuildRestAPISecret(deployment *carbitev1alpha1.CarbideDeployment, namespace, pgPassword, keycloakClientSecret string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-secret", RestAPIName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("rest-api-secret", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"DB_PASSWORD":            pgPassword,
			"KEYCLOAK_CLIENT_SECRET": keycloakClientSecret,
		},
	}
}

// BuildRestAPIServiceAccount creates the REST API ServiceAccount.
func BuildRestAPIServiceAccount(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ServiceAccount {
	automount := true
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RestAPIName,
			Namespace: namespace,
			Labels:    resources.DefaultLabels("rest-api", deployment),
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

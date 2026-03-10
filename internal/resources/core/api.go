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
	APIName = "carbide-api"
)

// BuildAPIConfigMap creates the carbide-api ConfigMap with TOML configuration.
func BuildAPIConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace string, pgHost string, pgPort int32) *corev1.ConfigMap {
	apiConfig := deployment.Spec.Core.API
	networkConfig := deployment.Spec.Network
	securityConfig := deployment.Spec.Core.Security

	port := apiConfig.Port
	if port == 0 {
		port = 1079
	}

	domain := networkConfig.Domain
	if domain == "" {
		domain = "carbide.local"
	}

	trustDomain := "carbide.local"
	if deployment.Spec.TLS != nil && deployment.Spec.TLS.SPIFFE != nil && deployment.Spec.TLS.SPIFFE.TrustDomain != "" {
		trustDomain = deployment.Spec.TLS.SPIFFE.TrustDomain
	}

	listenMode := "tls"
	rbacBypass := false
	if securityConfig != nil {
		if !securityConfig.TLSEnabled {
			listenMode = "plain"
		}
		rbacBypass = securityConfig.RBACBypass
	}

	// Build TOML config matching SNO reference
	tomlConfig := fmt.Sprintf(`listen = "[::]:%d"
listen_mode = "%s"
metrics_endpoint = "[::]:%d"

[tls]
root_cafile_path = "%s/ca.crt"
identity_pemfile_path = "%s/tls.crt"
identity_keyfile_path = "%s/tls.key"

[auth.trust]
spiffe_trust_domain = "%s"
spiffe_service_base_paths = ["/ns/%s/sa/"]
spiffe_machine_base_path = "/ns/%s/machine/"
`,
		port, listenMode, port+1,
		tls.CertDir, tls.CertDir, tls.CertDir,
		trustDomain,
		namespace, namespace,
	)

	if rbacBypass {
		tomlConfig += "\n[auth]\nbypass = true\n"
	}

	data := map[string]string{
		"carbide-api-config.toml": tomlConfig,
	}

	// Keep env-style data for backward compat with migration init container
	data["CARBIDE_DOMAIN"] = domain
	data["POSTGRES_HOST"] = pgHost
	data["POSTGRES_PORT"] = fmt.Sprintf("%d", pgPort)
	data["POSTGRES_DB"] = "carbide"

	if deployment.Spec.Profile == carbitev1alpha1.ProfileSite ||
		deployment.Spec.Profile == carbitev1alpha1.ProfileManagementWithSite {
		data["CARBIDE_NETWORK_INTERFACE"] = networkConfig.Interface
		data["CARBIDE_NETWORK_IP"] = networkConfig.IP
		data["CARBIDE_NETWORK_CIDR"] = networkConfig.AdminNetworkCIDR
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", APIName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("api-config", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: data,
	}
}

// BuildAPIDeployment creates the carbide-api Deployment.
func BuildAPIDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	apiConfig := deployment.Spec.Core.API

	replicas := apiConfig.Replicas
	if replicas == 0 {
		replicas = 1
	}

	port := apiConfig.Port
	if port == 0 {
		port = 1079
	}

	labels := resources.DefaultLabels("api", deployment)
	labels["app"] = APIName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-core:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.BMMCore != "" {
		imageName = deployment.Spec.Images.BMMCore
	}

	// Resource defaults matching SNO manifests
	res := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000m"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}
	if apiConfig.Resources != nil {
		res = *apiConfig.Resources
	}

	// Volume mounts for config and casbin policy
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/etc/carbide",
			ReadOnly:  true,
		},
		{
			Name:      "casbin-policy",
			MountPath: "/opt/carbide/casbin-policy.csv",
			SubPath:   "casbin-policy.csv",
			ReadOnly:  true,
		},
	}

	// Volumes
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-config", APIName),
					},
				},
			},
		},
		{
			Name: "casbin-policy",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "casbin-policy",
					},
				},
			},
		},
	}

	// Resolve the PG secret for the carbide user
	pgSecretName := infrastructure.ResolveUserSecret(deployment, "carbide")

	// Build env vars matching sno-manifests
	env := []corev1.EnvVar{
		{Name: "RUST_LOG", Value: "info"},
		{Name: "CARBIDE_API_DATABASE_URL", ValueFrom: secretKeyRef(pgSecretName, "uri")},
	}

	// Add Vault env vars if configured
	if deployment.Spec.Core.Vault != nil {
		kvMount := deployment.Spec.Core.Vault.KVMountPath
		if kvMount == "" {
			kvMount = "secrets"
		}
		if deployment.Spec.Core.Vault.Mode == carbitev1alpha1.ExternalMode && deployment.Spec.Core.Vault.Address != "" {
			env = append(env,
				corev1.EnvVar{Name: "VAULT_ADDR", Value: deployment.Spec.Core.Vault.Address},
				corev1.EnvVar{Name: "VAULT_KV_MOUNT_LOCATION", Value: kvMount},
				corev1.EnvVar{Name: "VAULT_PKI_MOUNT_LOCATION", Value: "unsupported"},
				corev1.EnvVar{Name: "VAULT_PKI_ROLE_NAME", Value: "unsupported"},
			)
			if deployment.Spec.Core.Vault.TokenSecretRef != nil {
				key := deployment.Spec.Core.Vault.TokenSecretRef.Key
				if key == "" {
					key = "token"
				}
				env = append(env, corev1.EnvVar{
					Name: "VAULT_TOKEN", ValueFrom: secretKeyRef(deployment.Spec.Core.Vault.TokenSecretRef.Name, key),
				})
			}
		} else if deployment.Spec.Core.Vault.Mode == carbitev1alpha1.ManagedMode {
			env = append(env,
				corev1.EnvVar{Name: "VAULT_ADDR", Value: fmt.Sprintf("http://vault.%s.svc:8200", namespace)},
				corev1.EnvVar{Name: "VAULT_TOKEN", ValueFrom: secretKeyRef("vault-unseal-secret", "root-token")},
				corev1.EnvVar{Name: "VAULT_KV_MOUNT_LOCATION", Value: kvMount},
				corev1.EnvVar{Name: "VAULT_PKI_MOUNT_LOCATION", Value: "unsupported"},
				corev1.EnvVar{Name: "VAULT_PKI_ROLE_NAME", Value: "unsupported"},
			)
		}
	}

	metricsPort := port + 1

	apiContainer := corev1.Container{
		Name:            "carbide-api",
		Image:           imageName,
		ImagePullPolicy: resources.GetImagePullPolicy(deployment),
		Command:         []string{"/opt/carbide/carbide-api"},
		Args:            []string{"run", "--config-path=/etc/carbide/carbide-api-config.toml"},
		Ports: []corev1.ContainerPort{
			{
				Name:          "grpc",
				ContainerPort: port,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          "metrics",
				ContainerPort: metricsPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:          env,
		VolumeMounts: volumeMounts,
		Resources:    res,
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/metrics",
					Port: intstr.FromInt32(metricsPort),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/metrics",
					Port: intstr.FromInt32(metricsPort),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       5,
		},
	}

	// Migration init container
	migrateContainer := corev1.Container{
		Name:            "db-migrations",
		Image:           imageName,
		ImagePullPolicy: resources.GetImagePullPolicy(deployment),
		Command:         []string{"/opt/carbide/carbide-api"},
		Args:            []string{"migrate", "--datastore=$(CARBIDE_API_DATABASE_URL)"},
		Env: []corev1.EnvVar{
			{Name: "CARBIDE_API_DATABASE_URL", ValueFrom: secretKeyRef(pgSecretName, "uri")},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: APIName,
		InitContainers:     []corev1.Container{migrateContainer},
		Containers:         []corev1.Container{apiContainer},
		Volumes:            volumes,
	}

	// Inject SPIFFE
	tls.InjectTLS(&podSpec, deployment)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      APIName,
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
					"app": APIName,
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

// BuildAPIService creates the carbide-api Service.
func BuildAPIService(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.Service {
	apiConfig := deployment.Spec.Core.API

	port := apiConfig.Port
	if port == 0 {
		port = 1079
	}

	labels := resources.DefaultLabels("api-service", deployment)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      APIName,
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
				"app": APIName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "metrics",
					Port:       port + 1,
					TargetPort: intstr.FromInt32(port + 1),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// BuildAPISecret creates the carbide-api Secret with database connection URL.
func BuildAPISecret(deployment *carbitev1alpha1.CarbideDeployment, namespace string, pgHost string, pgPort int32, pgPassword string) *corev1.Secret {
	dbURL := fmt.Sprintf("postgres://carbide:%s@%s:%d/carbide?sslmode=require", pgPassword, pgHost, pgPort)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-secret", APIName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("api-secret", deployment),
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
			"database-url": dbURL,
		},
	}
}

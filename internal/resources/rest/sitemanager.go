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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	SiteManagerName = "carbide-rest-site-manager"
)

// BuildSiteManagerDeployment creates the site-manager Deployment
// site-manager is an HTTP API service that manages Site CRD lifecycle
func BuildSiteManagerDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	labels := resources.DefaultLabels("site-manager", deployment)
	labels["app"] = SiteManagerName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/site-manager:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.SiteManager != "" {
		imageName = deployment.Spec.Images.SiteManager
	}

	replicas := int32(1)

	temporalNs := deployment.Spec.Rest.Temporal.Namespace
	if temporalNs == "" {
		temporalNs = "temporal"
	}

	env := []corev1.EnvVar{
		{
			Name:  "REST_API_URL",
			Value: fmt.Sprintf("http://carbide-rest-api.%s.svc:8080", namespace),
		},
		{
			Name:  "TEMPORAL_ENDPOINT",
			Value: fmt.Sprintf("temporal-frontend.%s.svc:7233", temporalNs),
		},
	}

	var args []string
	var volumeMounts []corev1.VolumeMount

	if tls.IsEnabled(deployment) {
		args = append(args,
			"--tls-cert-path", tls.CertDir+"/tls.crt",
			"--tls-key-path", tls.CertDir+"/tls.key",
		)
	}

	container := corev1.Container{
		Name:            "site-manager",
		Image:           imageName,
		ImagePullPolicy: resources.GetImagePullPolicy(deployment),
		Args:            args,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: 8080,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Env:          env,
		VolumeMounts: volumeMounts,
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/readyz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
		},
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: SiteManagerName,
		Containers:         []corev1.Container{container},
	}

	tls.InjectTLS(&podSpec, deployment)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SiteManagerName,
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
					"app": SiteManagerName,
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

// BuildSiteManagerService creates the site-manager Service
func BuildSiteManagerService(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.Service {
	labels := resources.DefaultLabels("site-manager", deployment)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SiteManagerName,
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
				"app": SiteManagerName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

// BuildSiteManagerServiceAccount creates RBAC resources for site-manager
// site-manager needs permissions to manage Site CRDs
func BuildSiteManagerServiceAccount(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ServiceAccount {
	labels := resources.DefaultLabels("site-manager", deployment)

	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SiteManagerName,
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
	}
}

// BuildSiteManagerRole creates Role with Site CRD permissions
func BuildSiteManagerRole(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *rbacv1.Role {
	labels := resources.DefaultLabels("site-manager", deployment)

	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SiteManagerName,
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
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"forge.nvidia.io"},
				Resources: []string{"sites"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"forge.nvidia.io"},
				Resources: []string{"sites/status"},
				Verbs:     []string{"get", "update", "patch"},
			},
		},
	}
}

// BuildSiteManagerRoleBinding binds Role to ServiceAccount
func BuildSiteManagerRoleBinding(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *rbacv1.RoleBinding {
	labels := resources.DefaultLabels("site-manager", deployment)

	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SiteManagerName,
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
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      SiteManagerName,
				Namespace: namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     SiteManagerName,
		},
	}
}

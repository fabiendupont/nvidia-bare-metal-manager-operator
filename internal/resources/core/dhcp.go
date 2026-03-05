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

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	DHCPName = "carbide-dhcp"
)

// BuildDHCPDaemonSet creates the DHCP server DaemonSet.
func BuildDHCPDaemonSet(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.DaemonSet {
	labels := resources.DefaultLabels("dhcp", deployment)
	labels["app"] = DHCPName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-core:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.BMMCore != "" {
		imageName = deployment.Spec.Images.BMMCore
	}

	privileged := true
	apiPort := deployment.Spec.Core.API.Port
	if apiPort == 0 {
		apiPort = 1079
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
	if deployment.Spec.Core.DHCP.Resources != nil {
		res = *deployment.Spec.Core.DHCP.Resources
	}

	env := []corev1.EnvVar{
		{
			Name:  "RUST_LOG",
			Value: "info",
		},
		{
			Name:  "BMM_API_URL",
			Value: fmt.Sprintf("https://carbide-api.%s.svc:%d", namespace, apiPort),
		},
		{
			Name:  "NETWORK_INTERFACE",
			Value: deployment.Spec.Network.Interface,
		},
	}

	var volumeMounts []corev1.VolumeMount

	// Add SPIFFE cert env vars and mount
	if tls.IsEnabled(deployment) {
		env = append(env, tls.CertEnvVars()...)
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: DHCPName,
		HostNetwork:        true,
		DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
		NodeSelector: map[string]string{
			"node-role.kubernetes.io/control-plane": "",
		},
		Tolerations: []corev1.Toleration{
			{
				Key:      "node-role.kubernetes.io/master",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			},
		},
		Containers: []corev1.Container{
			{
				Name:            "dhcp",
				Image:           imageName,
				ImagePullPolicy: resources.GetImagePullPolicy(deployment),
				Command:         []string{"/usr/local/bin/carbide-dhcp"},
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"NET_ADMIN", "NET_RAW", "NET_BIND_SERVICE"},
					},
					Privileged: &privileged,
				},
				Env:          env,
				VolumeMounts: volumeMounts,
				Resources:    res,
			},
		},
	}

	// Inject SPIFFE
	tls.InjectTLS(&podSpec, deployment)

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DHCPName,
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
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": DHCPName,
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

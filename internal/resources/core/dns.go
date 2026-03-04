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
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/spiffe"
)

const (
	DNSName = "carbide-dns"
)

// BuildDNSConfigMap creates the DNS server TOML configuration ConfigMap.
func BuildDNSConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ConfigMap {
	apiPort := deployment.Spec.Core.API.Port
	if apiPort == 0 {
		apiPort = 1079
	}

	networkIP := deployment.Spec.Network.IP
	dnsPort := deployment.Spec.Core.DNS.Port
	if dnsPort == 0 {
		dnsPort = 53
	}

	legacyListen := fmt.Sprintf("%s:%d", networkIP, dnsPort)
	if networkIP == "" {
		legacyListen = fmt.Sprintf("0.0.0.0:%d", dnsPort)
	}

	tomlConfig := fmt.Sprintf(`carbide_uri = "https://carbide-api:%d"
legacy_listen = "%s"
forge_root_ca = "%s/ca.crt"
client_cert_path = "%s/tls.crt"
client_key_path = "%s/tls.key"
`,
		apiPort, legacyListen,
		spiffe.CertDir, spiffe.CertDir, spiffe.CertDir,
	)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", DNSName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("dns-config", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: map[string]string{
			"carbide-dns-config.toml": tomlConfig,
		},
	}
}

// BuildDNSDaemonSet creates the DNS server DaemonSet.
func BuildDNSDaemonSet(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.DaemonSet {
	labels := resources.DefaultLabels("dns", deployment)
	labels["app"] = DNSName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-core:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.BMMCore != "" {
		imageName = deployment.Spec.Images.BMMCore
	}

	port := deployment.Spec.Core.DNS.Port
	if port == 0 {
		port = 53
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
	if deployment.Spec.Core.DNS.Resources != nil {
		res = *deployment.Spec.Core.DNS.Resources
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "dns-config",
			MountPath: "/etc/carbide/carbide-dns-config.toml",
			SubPath:   "carbide-dns-config.toml",
			ReadOnly:  true,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "dns-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-config", DNSName),
					},
				},
			},
		},
	}

	// Add SPIFFE cert mount for app container
	if spiffe.IsEnabled(deployment) {
		volumeMounts = append(volumeMounts, spiffe.SpiffeCertVolumeMount())
	}

	podSpec := corev1.PodSpec{
		ServiceAccountName: DNSName,
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
				Name:            "dns",
				Image:           imageName,
				ImagePullPolicy: resources.GetImagePullPolicy(deployment),
				Command:         []string{"/usr/local/bin/carbide-dns"},
				Ports: []corev1.ContainerPort{
					{
						Name:          "dns-tcp",
						ContainerPort: port,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "dns-udp",
						ContainerPort: port,
						Protocol:      corev1.ProtocolUDP,
					},
				},
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"NET_BIND_SERVICE"},
					},
				},
				VolumeMounts: volumeMounts,
				Resources:    res,
			},
		},
		Volumes: volumes,
	}

	// Inject SPIFFE
	spiffe.InjectSpiffe(&podSpec, deployment)

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DNSName,
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
					"app": DNSName,
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

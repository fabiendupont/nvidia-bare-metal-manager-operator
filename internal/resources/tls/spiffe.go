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

package tls

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

const (
	SpiffeSocketDir = "/var/run/secrets/spiffe.io"
	HelperConfigDir = "/etc/spiffe-helper"
)

// DetectSPIRE checks if the SPIRE CSI driver is available in the cluster.
func DetectSPIRE(ctx context.Context, c client.Client) (bool, error) {
	csiDriver := &unstructured.Unstructured{}
	csiDriver.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "storage.k8s.io",
		Version: "v1",
		Kind:    "CSIDriver",
	})

	if err := c.Get(ctx, types.NamespacedName{Name: "csi.spiffe.io"}, csiDriver); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// BuildSpiffeHelperConfigMap creates the spiffe-helper ConfigMap with init and daemon configs.
func BuildSpiffeHelperConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spiffe-helper-config",
			Namespace: namespace,
			Labels:    resources.DefaultLabels("spiffe-helper", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: map[string]string{
			"helper-init.conf": `agent_address = "/var/run/secrets/spiffe.io/spire-agent.sock"
cert_dir = "/var/run/secrets/tls"
svid_file_name = "tls.crt"
svid_key_file_name = "tls.key"
svid_bundle_file_name = "ca.crt"
daemon_mode = false
`,
			"helper.conf": `agent_address = "/var/run/secrets/spiffe.io/spire-agent.sock"
cert_dir = "/var/run/secrets/tls"
svid_file_name = "tls.crt"
svid_key_file_name = "tls.key"
svid_bundle_file_name = "ca.crt"
daemon_mode = true
`,
		},
	}
}

// BuildClusterSPIFFEID creates a ClusterSPIFFEID CR for a workload.
func BuildClusterSPIFFEID(
	deployment *carbitev1alpha1.CarbideDeployment,
	name string,
	namespace string,
	podLabelApp string,
	dnsSANs []string,
) *unstructured.Unstructured {
	trustDomain := "carbide.local"
	className := "zero-trust-workload-identity-manager-spire"
	if deployment.Spec.TLS != nil && deployment.Spec.TLS.SPIFFE != nil {
		if deployment.Spec.TLS.SPIFFE.TrustDomain != "" {
			trustDomain = deployment.Spec.TLS.SPIFFE.TrustDomain
		}
		if deployment.Spec.TLS.SPIFFE.ClassName != "" {
			className = deployment.Spec.TLS.SPIFFE.ClassName
		}
	}

	spec := map[string]interface{}{
		"className":        className,
		"spiffeIDTemplate": fmt.Sprintf("spiffe://%s/ns/{{ .PodMeta.Namespace }}/sa/{{ .PodSpec.ServiceAccountName }}", trustDomain),
		"podSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"app": podLabelApp,
			},
		},
		"namespaceSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"kubernetes.io/metadata.name": namespace,
			},
		},
	}

	if len(dnsSANs) > 0 {
		sans := make([]interface{}, len(dnsSANs))
		for i, s := range dnsSANs {
			sans[i] = s
		}
		spec["dnsNameTemplates"] = sans
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "spire.spiffe.io/v1alpha1",
			"kind":       "ClusterSPIFFEID",
			"metadata": map[string]interface{}{
				"name":   name,
				"labels": resources.DefaultLabelsUnstructured("spiffe-id", deployment),
			},
			"spec": spec,
		},
	}

	obj.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
			Group:   carbitev1alpha1.GroupVersion.Group,
			Version: carbitev1alpha1.GroupVersion.Version,
			Kind:    "CarbideDeployment",
		}),
	})

	return obj
}

// SpiffeInitContainer returns the spiffe-helper init container.
func SpiffeInitContainer(helperImage string) corev1.Container {
	return corev1.Container{
		Name:  "spiffe-helper-init",
		Image: helperImage,
		Args:  []string{"-config", "/etc/spiffe-helper/helper-init.conf"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "spiffe-workload-api",
				MountPath: SpiffeSocketDir,
				ReadOnly:  true,
			},
			{
				Name:      "tls-certs",
				MountPath: CertDir,
			},
			{
				Name:      "spiffe-helper-config",
				MountPath: HelperConfigDir,
				ReadOnly:  true,
			},
		},
	}
}

// SpiffeSidecarContainer returns the spiffe-helper sidecar container.
func SpiffeSidecarContainer(helperImage string) corev1.Container {
	return corev1.Container{
		Name:  "spiffe-helper",
		Image: helperImage,
		Args:  []string{"-config", "/etc/spiffe-helper/helper.conf"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "spiffe-workload-api",
				MountPath: SpiffeSocketDir,
				ReadOnly:  true,
			},
			{
				Name:      "tls-certs",
				MountPath: CertDir,
			},
			{
				Name:      "spiffe-helper-config",
				MountPath: HelperConfigDir,
				ReadOnly:  true,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
	}
}

// SpiffeVolumes returns the volumes needed for SPIFFE injection.
func SpiffeVolumes() []corev1.Volume {
	readOnly := true
	return []corev1.Volume{
		{
			Name: "spiffe-workload-api",
			VolumeSource: corev1.VolumeSource{
				CSI: &corev1.CSIVolumeSource{
					Driver:   "csi.spiffe.io",
					ReadOnly: &readOnly,
				},
			},
		},
		{
			Name: "tls-certs",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "spiffe-helper-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "spiffe-helper-config",
					},
				},
			},
		},
	}
}

// InjectSpiffe modifies a PodSpec in-place to add SPIFFE init container, sidecar, and volumes.
func InjectSpiffe(podSpec *corev1.PodSpec, deployment *carbitev1alpha1.CarbideDeployment) {
	if deployment.Spec.TLS == nil || deployment.Spec.TLS.Mode != carbitev1alpha1.TLSModeSpiffe {
		return
	}

	helperImage := "ghcr.io/nvidia/spiffe-helper:latest"
	if deployment.Spec.TLS.SPIFFE != nil && deployment.Spec.TLS.SPIFFE.HelperImage != "" {
		helperImage = deployment.Spec.TLS.SPIFFE.HelperImage
	}

	// Prepend init container
	podSpec.InitContainers = append(
		[]corev1.Container{SpiffeInitContainer(helperImage)},
		podSpec.InitContainers...,
	)

	// Append sidecar
	podSpec.Containers = append(podSpec.Containers, SpiffeSidecarContainer(helperImage))

	// Add cert mount to app containers (not the sidecar we just added)
	certMount := corev1.VolumeMount{
		Name:      "tls-certs",
		MountPath: CertDir,
		ReadOnly:  true,
	}
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == "spiffe-helper" {
			continue // sidecar already has its own mount
		}
		podSpec.Containers[i].VolumeMounts = append(podSpec.Containers[i].VolumeMounts, certMount)
	}

	// Append volumes
	podSpec.Volumes = append(podSpec.Volumes, SpiffeVolumes()...)
}

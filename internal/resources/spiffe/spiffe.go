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

// Package spiffe provides backward-compatible wrappers around the tls package.
// New code should use internal/resources/tls directly.
package spiffe

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

// CertDir is the standard TLS certificate directory (delegated to tls package).
const CertDir = tls.CertDir

// IsEnabled returns whether TLS/SPIFFE is enabled in the deployment.
func IsEnabled(deployment *carbitev1alpha1.CarbideDeployment) bool {
	return tls.IsEnabled(deployment)
}

// SpiffeCertVolumeMount returns a read-only volume mount for certs (for app containers).
func SpiffeCertVolumeMount() corev1.VolumeMount {
	return tls.CertVolumeMount()
}

// SpiffeCertEnvVars returns env vars pointing to cert paths.
func SpiffeCertEnvVars() []corev1.EnvVar {
	return tls.CertEnvVars()
}

// InjectSpiffe modifies a PodSpec in-place to add TLS support (delegates to tls.InjectTLS).
func InjectSpiffe(podSpec *corev1.PodSpec, deployment *carbitev1alpha1.CarbideDeployment) {
	tls.InjectTLS(podSpec, deployment)
}

// BuildSpiffeHelperConfigMap creates the spiffe-helper ConfigMap (delegates to tls package).
func BuildSpiffeHelperConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ConfigMap {
	return tls.BuildSpiffeHelperConfigMap(deployment, namespace)
}

// BuildClusterSPIFFEID creates a ClusterSPIFFEID CR for a workload (delegates to tls package).
func BuildClusterSPIFFEID(
	deployment *carbitev1alpha1.CarbideDeployment,
	name string,
	namespace string,
	podLabelApp string,
	dnsSANs []string,
) *unstructured.Unstructured {
	return tls.BuildClusterSPIFFEID(deployment, name, namespace, podLabelApp, dnsSANs)
}

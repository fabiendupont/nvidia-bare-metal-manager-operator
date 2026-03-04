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
	corev1 "k8s.io/api/core/v1"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

const (
	CertDir = "/var/run/secrets/tls"
)

// IsEnabled returns whether TLS is enabled in the deployment.
func IsEnabled(deployment *carbitev1alpha1.CarbideDeployment) bool {
	return deployment.Spec.TLS != nil
}

// IsSpiffeMode returns whether SPIFFE mode is active.
func IsSpiffeMode(deployment *carbitev1alpha1.CarbideDeployment) bool {
	return deployment.Spec.TLS != nil && deployment.Spec.TLS.Mode == carbitev1alpha1.TLSModeSpiffe
}

// IsCertManagerMode returns whether cert-manager mode is active.
func IsCertManagerMode(deployment *carbitev1alpha1.CarbideDeployment) bool {
	return deployment.Spec.TLS != nil && deployment.Spec.TLS.Mode == carbitev1alpha1.TLSModeCertManager
}

// GetCertDir returns the standard TLS certificate directory.
func GetCertDir() string {
	return CertDir
}

// CertEnvVars returns env vars pointing to TLS cert paths (same for both modes).
func CertEnvVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "FORGE_ROOT_CAFILE_PATH", Value: CertDir + "/ca.crt"},
		{Name: "FORGE_CLIENT_CERT_PATH", Value: CertDir + "/tls.crt"},
		{Name: "FORGE_CLIENT_KEY_PATH", Value: CertDir + "/tls.key"},
	}
}

// CertVolumeMount returns a read-only volume mount for TLS certs (for app containers).
func CertVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      "tls-certs",
		MountPath: CertDir,
		ReadOnly:  true,
	}
}

// InjectTLS dispatches to SPIFFE or cert-manager injection based on mode.
func InjectTLS(podSpec *corev1.PodSpec, deployment *carbitev1alpha1.CarbideDeployment) {
	if deployment.Spec.TLS == nil {
		return
	}

	switch deployment.Spec.TLS.Mode {
	case carbitev1alpha1.TLSModeSpiffe:
		InjectSpiffe(podSpec, deployment)
	case carbitev1alpha1.TLSModeCertManager:
		InjectCertManager(podSpec, deployment)
	}
}

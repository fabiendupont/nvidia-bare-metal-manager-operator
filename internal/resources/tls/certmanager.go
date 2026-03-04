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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

// DetectCertManager checks if cert-manager is available by looking for the Certificate CRD.
func DetectCertManager(ctx context.Context, c client.Client) (bool, error) {
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})

	if err := c.Get(ctx, types.NamespacedName{Name: "certificates.cert-manager.io"}, crd); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// BuildCertificate creates a cert-manager Certificate CR for a service.
func BuildCertificate(
	deployment *carbitev1alpha1.CarbideDeployment,
	name string,
	namespace string,
	dnsNames []string,
) *unstructured.Unstructured {
	cmConfig := deployment.Spec.TLS.CertManager
	secretName := name + "-tls"

	issuerGroup := cmConfig.IssuerRef.Group
	if issuerGroup == "" {
		issuerGroup = "cert-manager.io"
	}

	cert := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cert-manager.io/v1",
			"kind":       "Certificate",
			"metadata": map[string]interface{}{
				"name":      name + "-cert",
				"namespace": namespace,
				"labels":    resources.DefaultLabelsUnstructured("tls-cert", deployment),
			},
			"spec": map[string]interface{}{
				"secretName":  secretName,
				"duration":    "2160h", // 90 days
				"renewBefore": "360h",  // 15 days
				"dnsNames":    interfaceSlice(dnsNames),
				"issuerRef": map[string]interface{}{
					"name":  cmConfig.IssuerRef.Name,
					"kind":  cmConfig.IssuerRef.Kind,
					"group": issuerGroup,
				},
				"usages": []interface{}{
					"digital signature",
					"key encipherment",
					"server auth",
					"client auth",
				},
			},
		},
	}

	cert.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
			Group:   carbitev1alpha1.GroupVersion.Group,
			Version: carbitev1alpha1.GroupVersion.Version,
			Kind:    "CarbideDeployment",
		}),
	})

	return cert
}

// InjectCertManager modifies a PodSpec in-place to mount cert-manager secret as TLS certs.
func InjectCertManager(podSpec *corev1.PodSpec, deployment *carbitev1alpha1.CarbideDeployment) {
	if deployment.Spec.TLS == nil || deployment.Spec.TLS.Mode != carbitev1alpha1.TLSModeCertManager {
		return
	}

	// The cert-manager secret name is derived from the service account name
	// Each deployment should have its own certificate and secret
	saName := podSpec.ServiceAccountName
	if saName == "" {
		saName = "default"
	}
	secretName := saName + "-tls"

	// Add cert volume
	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name: "tls-certs",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	})

	// Add cert mount to all containers
	certMount := corev1.VolumeMount{
		Name:      "tls-certs",
		MountPath: CertDir,
		ReadOnly:  true,
	}
	for i := range podSpec.Containers {
		podSpec.Containers[i].VolumeMounts = append(podSpec.Containers[i].VolumeMounts, certMount)
	}
}

func interfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

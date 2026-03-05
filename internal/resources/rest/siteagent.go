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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	SiteAgentName = "carbide-rest-site-agent"
)

// BuildSiteAgentConfigMap creates the site agent ConfigMap.
func BuildSiteAgentConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace, temporalEndpoint string) *corev1.ConfigMap {
	siteAgentConfig := deployment.Spec.Rest.SiteAgent

	hubEndpoint := temporalEndpoint
	if siteAgentConfig != nil && siteAgentConfig.HubTemporalEndpoint != "" {
		hubEndpoint = siteAgentConfig.HubTemporalEndpoint
	}

	apiPort := deployment.Spec.Core.API.Port
	if apiPort == 0 {
		apiPort = 1079
	}

	data := map[string]string{
		"ESA_PORT":                   "8080",
		"CARBIDE_ADDRESS":            fmt.Sprintf("carbide-api.%s.svc:%d", deployment.Spec.Core.Namespace, apiPort),
		"CARBIDE_SEC_OPT":            "2",
		"TEMPORAL_HOST":              "temporal-frontend",
		"TEMPORAL_PORT":              "7233",
		"TEMPORAL_PUBLISH_NAMESPACE": "site",
		"ENABLE_TLS":                 "true",
		"TEMPORAL_CERT_PATH":         tls.CertDir,
	}

	if hubEndpoint != "" {
		data["TEMPORAL_ENDPOINT"] = hubEndpoint
	}

	// Add SPIFFE cert paths
	if tls.IsEnabled(deployment) {
		data["CARBIDE_CA_CERT_PATH"] = tls.CertDir + "/ca.crt"
		data["CARBIDE_CLIENT_CERT_PATH"] = tls.CertDir + "/tls.crt"
		data["CARBIDE_CLIENT_KEY_PATH"] = tls.CertDir + "/tls.key"
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-config", SiteAgentName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("site-agent-config", deployment),
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

// BuildSiteAgentDeployment creates the site agent Deployment.
func BuildSiteAgentDeployment(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *appsv1.Deployment {
	labels := resources.DefaultLabels("site-agent", deployment)
	labels["app"] = SiteAgentName

	registry := resources.GetImageRegistry(deployment)
	imageName := fmt.Sprintf("%s/carbide-rest-site-agent:%s", registry, deployment.Spec.Version)
	if deployment.Spec.Images != nil && deployment.Spec.Images.SiteAgent != "" {
		imageName = deployment.Spec.Images.SiteAgent
	}

	replicas := int32(1)

	var volumeMounts []corev1.VolumeMount

	podSpec := corev1.PodSpec{
		ServiceAccountName: SiteAgentName,
		Containers: []corev1.Container{
			{
				Name:            "agent",
				Image:           imageName,
				ImagePullPolicy: resources.GetImagePullPolicy(deployment),
				EnvFrom: []corev1.EnvFromSource{
					{
						ConfigMapRef: &corev1.ConfigMapEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: fmt.Sprintf("%s-config", SiteAgentName),
							},
						},
					},
				},
				VolumeMounts: volumeMounts,
			},
		},
	}

	tls.InjectTLS(&podSpec, deployment)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SiteAgentName,
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
					"app": SiteAgentName,
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

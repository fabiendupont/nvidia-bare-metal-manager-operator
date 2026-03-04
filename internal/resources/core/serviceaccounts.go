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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

// Per-service ServiceAccount name constants
const (
	SANameAPI  = "carbide-api"
	SANameDHCP = "carbide-dhcp"
	SANameDNS  = "carbide-dns"
	SANamePXE  = "carbide-pxe"
	SANameRLA  = "carbide-rla"
	SANamePSM  = "carbide-psm"
)

// BuildServiceAccount creates a ServiceAccount with automountServiceAccountToken enabled.
func BuildServiceAccount(name, namespace string, deployment *carbitev1alpha1.CarbideDeployment) *corev1.ServiceAccount {
	automount := true
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    resources.DefaultLabels(name, deployment),
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

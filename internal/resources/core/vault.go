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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

const (
	VaultName          = "vault"
	VaultHelmJobName   = "vault-helm-install"
	VaultInitJobName   = "vault-init"
	VaultUnsealSecret  = "vault-unseal-secret"
	VaultValuesMapName = "vault-helm-values"
)

// BuildVaultHelmValuesConfigMap creates a ConfigMap with Vault Helm chart values.
func BuildVaultHelmValuesConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ConfigMap {
	vaultConfig := deployment.Spec.Core.Vault

	version := "1.15.6"
	if vaultConfig != nil && vaultConfig.Version != "" {
		version = vaultConfig.Version
	}

	valuesYAML := fmt.Sprintf(`global:
  enabled: true
server:
  image:
    tag: "%s"
  standalone:
    enabled: true
  dataStorage:
    enabled: true
    size: 10Gi
  ha:
    enabled: false
ui:
  enabled: false
injector:
  enabled: false
`, version)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VaultValuesMapName,
			Namespace: namespace,
			Labels:    resources.DefaultLabels("vault-helm-values", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: map[string]string{
			"values.yaml": valuesYAML,
		},
	}
}

// BuildVaultHelmJob creates a Job that runs helm install for Vault.
func BuildVaultHelmJob(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *batchv1.Job {
	labels := resources.DefaultLabels("vault-helm", deployment)
	backoffLimit := int32(5)
	ttlAfterFinished := int32(86400)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VaultHelmJobName,
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
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: "vault-helm-installer",
					Containers: []corev1.Container{
						{
							Name:            "helm",
							Image:           "alpine/helm:latest",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/sh"},
							Args: []string{
								"-c",
								fmt.Sprintf(`helm repo add hashicorp https://helm.releases.hashicorp.com && \
helm repo update && \
helm upgrade --install vault hashicorp/vault \
  -n %s \
  -f /values/values.yaml \
  --wait --timeout 10m`, namespace),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "values",
									MountPath: "/values",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "values",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: VaultValuesMapName,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// BuildVaultInitJob creates a Job that initializes Vault and stores unseal keys.
func BuildVaultInitJob(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *batchv1.Job {
	labels := resources.DefaultLabels("vault-init", deployment)
	backoffLimit := int32(10)
	ttlAfterFinished := int32(86400)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VaultInitJobName,
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
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: "vault-helm-installer",
					Containers: []corev1.Container{
						{
							Name:            "vault-init",
							Image:           "hashicorp/vault:latest",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"/bin/sh"},
							Args: []string{
								"-c",
								fmt.Sprintf(`
export VAULT_ADDR=http://vault.%s.svc:8200

echo "Waiting for Vault to be reachable..."
until vault status -format=json 2>/dev/null | grep -q '"initialized"'; do
  echo "Still waiting..."
  sleep 5
done

# Check if already initialized
INIT_STATUS=$(vault status -format=json 2>/dev/null | grep '"initialized"' | tr -d ' ",')
if echo "$INIT_STATUS" | grep -q "true"; then
  echo "Vault already initialized"
  exit 0
fi

echo "Initializing Vault..."
INIT_OUTPUT=$(vault operator init -key-shares=1 -key-threshold=1 -format=json)

UNSEAL_KEY=$(echo "$INIT_OUTPUT" | grep -o '"unseal_keys_b64":\[\"[^"]*\"' | sed 's/.*\["//' | sed 's/".*//')
ROOT_TOKEN=$(echo "$INIT_OUTPUT" | grep -o '"root_token":"[^"]*"' | sed 's/.*:"//' | sed 's/".*//')

echo "Unsealing Vault..."
vault operator unseal "$UNSEAL_KEY"

# Create K8s secret with unseal key and root token
cat <<EOSECRET | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
type: Opaque
stringData:
  unseal-key: "$UNSEAL_KEY"
  root-token: "$ROOT_TOKEN"
EOSECRET

echo "Vault initialized and unsealed successfully"
`, namespace, VaultUnsealSecret, namespace),
							},
						},
					},
				},
			},
		},
	}
}

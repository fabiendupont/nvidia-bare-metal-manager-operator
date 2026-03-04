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
	"crypto/rand"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

const (
	KeycloakName = "keycloak"
)

// BuildKeycloakInstance creates a Keycloak CR for RHBK operator
func BuildKeycloakInstance(deployment *carbitev1alpha1.CarbideDeployment, namespace string) (*unstructured.Unstructured, error) {
	keycloakConfig := deployment.Spec.Rest.Keycloak

	// Build Keycloak CR using unstructured since RHBK operator CRDs may not be imported
	keycloak := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.keycloak.org/v2alpha1",
			"kind":       "Keycloak",
			"metadata": map[string]interface{}{
				"name":      KeycloakName,
				"namespace": namespace,
				"labels":    resources.DefaultLabelsUnstructured("keycloak", deployment),
			},
			"spec": map[string]interface{}{
				"instances": int64(1),
				"http": map[string]interface{}{
					"httpEnabled": true,
				},
				"hostname": map[string]interface{}{
					"hostname": fmt.Sprintf("keycloak.%s.svc", namespace),
				},
			},
		},
	}

	// Add resources if specified
	if keycloakConfig.Resources != nil {
		spec := keycloak.Object["spec"].(map[string]interface{})
		spec["resources"] = convertResources(keycloakConfig.Resources)
	}

	// Set owner reference
	keycloak.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
			Group:   carbitev1alpha1.GroupVersion.Group,
			Version: carbitev1alpha1.GroupVersion.Version,
			Kind:    "CarbideDeployment",
		}),
	})

	return keycloak, nil
}

// BuildKeycloakRealmImport creates a KeycloakRealmImport CR for RHBK operator
func BuildKeycloakRealmImport(deployment *carbitev1alpha1.CarbideDeployment, namespace string) (*unstructured.Unstructured, error) {
	keycloakConfig := deployment.Spec.Rest.Keycloak
	realm := keycloakConfig.Realm

	// Build KeycloakRealmImport CR
	realmImport := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.keycloak.org/v2alpha1",
			"kind":       "KeycloakRealmImport",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("%s-%s", KeycloakName, realm),
				"namespace": namespace,
				"labels":    resources.DefaultLabelsUnstructured("keycloak-realm", deployment),
			},
			"spec": map[string]interface{}{
				"keycloakCRName": KeycloakName,
				"realm": map[string]interface{}{
					"id":          realm,
					"realm":       realm,
					"enabled":     true,
					"displayName": "BMM",
					"clients": []interface{}{
						map[string]interface{}{
							"clientId":                  "carbide-rest-api",
							"enabled":                   true,
							"protocol":                  "openid-connect",
							"publicClient":              false,
							"serviceAccountsEnabled":    true,
							"directAccessGrantsEnabled": true,
							"standardFlowEnabled":       true,
							"redirectUris": []interface{}{
								"*",
							},
							"webOrigins": []interface{}{
								"*",
							},
						},
					},
					"roles": map[string]interface{}{
						"realm": []interface{}{
							map[string]interface{}{
								"name":        "carbide-admin",
								"description": "BMM Administrator",
							},
							map[string]interface{}{
								"name":        "carbide-user",
								"description": "BMM User",
							},
						},
					},
					"users": []interface{}{
						map[string]interface{}{
							"username": "admin",
							"enabled":  true,
							"realmRoles": []interface{}{
								"carbide-admin",
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference
	realmImport.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
			Group:   carbitev1alpha1.GroupVersion.Group,
			Version: carbitev1alpha1.GroupVersion.Version,
			Kind:    "CarbideDeployment",
		}),
	})

	return realmImport, nil
}

// GetKeycloakURL returns the internal Keycloak URL
func GetKeycloakURL(namespace string) string {
	return fmt.Sprintf("http://%s-service.%s.svc:8080", KeycloakName, namespace)
}

// GetKeycloakClientSecretName returns the name of the Keycloak client secret
func GetKeycloakClientSecretName() string {
	return fmt.Sprintf("%s-client-secret-carbide-rest-api", KeycloakName)
}

// BuildKeycloakAdminSecret creates a Secret for the Keycloak admin password.
// If AdminPasswordSecretRef is set, returns nil (the user manages the secret).
// Otherwise generates a random password.
func BuildKeycloakAdminSecret(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.Secret {
	keycloakConfig := deployment.Spec.Rest.Keycloak

	// If user provided a secret reference, don't create one
	if keycloakConfig.AdminPasswordSecretRef != nil && keycloakConfig.AdminPasswordSecretRef.Name != "" {
		return nil
	}

	// Generate random password
	password := generateRandomPassword(24)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-admin-password", KeycloakName),
			Namespace: namespace,
			Labels:    resources.DefaultLabels("keycloak-admin", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": password,
		},
	}
}

// BuildRestAPIAuthConfig generates the auth section for REST API ConfigMap based on Keycloak mode.
func BuildRestAPIAuthConfig(deployment *carbitev1alpha1.CarbideDeployment, namespace string) string {
	if deployment.Spec.Rest == nil {
		return ""
	}

	keycloakConfig := deployment.Spec.Rest.Keycloak

	switch keycloakConfig.Mode {
	case carbitev1alpha1.AuthModeManaged:
		keycloakURL := GetKeycloakURL(namespace)
		return fmt.Sprintf(`auth:
  - name: keycloak
    origin: 2
    url: %s/realms/%s/protocol/openid-connect/certs
`, keycloakURL, keycloakConfig.Realm)

	case carbitev1alpha1.AuthModeExternal:
		if len(keycloakConfig.AuthProviders) == 0 {
			return ""
		}
		result := "auth:\n"
		for i, provider := range keycloakConfig.AuthProviders {
			result += fmt.Sprintf(`  - name: %s
    origin: %d
    url: %s
`, provider.Name, i+2, provider.JWKSURL)
		}
		return result

	case carbitev1alpha1.AuthModeDisabled:
		return "# auth disabled\n"

	default:
		return ""
	}
}

func generateRandomPassword(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		// Fallback - should never happen
		return "change-me-immediately"
	}
	return base64.RawURLEncoding.EncodeToString(b)[:length]
}

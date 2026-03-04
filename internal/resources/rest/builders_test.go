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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

func newTestDeployment() *carbitev1alpha1.CarbideDeployment {
	return &carbitev1alpha1.CarbideDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
		Spec: carbitev1alpha1.CarbideDeploymentSpec{
			Profile: carbitev1alpha1.ProfileManagement,
			Version: "1.0.0",
			Network: carbitev1alpha1.NetworkConfig{
				Domain: "carbide.local",
			},
			Core: carbitev1alpha1.CoreConfig{},
			Rest: &carbitev1alpha1.RestConfig{
				Enabled: true,
				Temporal: carbitev1alpha1.TemporalConfig{
					Mode:         carbitev1alpha1.ManagedMode,
					ChartVersion: "0.73.1",
					Replicas:     1,
				},
				Keycloak: carbitev1alpha1.KeycloakConfig{
					Mode:  carbitev1alpha1.AuthModeManaged,
					Realm: "carbide",
				},
				RestAPI: carbitev1alpha1.RestAPIConfig{
					Port:     8080,
					Replicas: 1,
				},
			},
		},
	}
}

// --- Temporal tests ---

func TestBuildTemporalHelmValuesConfigMap(t *testing.T) {
	deployment := newTestDeployment()

	cm := BuildTemporalHelmValuesConfigMap(deployment, "test-ns", "pg-host.svc", 5432, "pg-secret")

	// Verify name
	if cm.Name != TemporalValuesConfigMap {
		t.Errorf("expected name %q, got %q", TemporalValuesConfigMap, cm.Name)
	}

	valuesYAML, ok := cm.Data["values.yaml"]
	if !ok {
		t.Fatal("expected values.yaml key in ConfigMap data")
	}

	// Verify postgres persistence config
	if !strings.Contains(valuesYAML, "driver: postgres12") {
		t.Error("expected values.yaml to contain postgres12 driver")
	}
	if !strings.Contains(valuesYAML, "host: pg-host.svc") {
		t.Error("expected values.yaml to contain pg host")
	}
	if !strings.Contains(valuesYAML, "database: temporal") {
		t.Error("expected values.yaml to contain temporal database")
	}
	if !strings.Contains(valuesYAML, "database: temporal_visibility") {
		t.Error("expected values.yaml to contain temporal_visibility database")
	}
	if !strings.Contains(valuesYAML, "existingSecret: pg-secret") {
		t.Error("expected values.yaml to contain pg secret reference")
	}

	// Verify cassandra/mysql/external-postgres are disabled
	if !strings.Contains(valuesYAML, "cassandra:\n  enabled: false") {
		t.Error("expected cassandra to be disabled")
	}
}

func TestBuildTemporalHelmJob(t *testing.T) {
	deployment := newTestDeployment()
	deployment.Spec.Rest.Temporal.ChartVersion = "0.73.1"

	job := BuildTemporalHelmJob(deployment, "test-ns")

	// Verify name
	if job.Name != TemporalHelmJobName {
		t.Errorf("expected name %q, got %q", TemporalHelmJobName, job.Name)
	}

	// Verify namespace
	if job.Namespace != "test-ns" {
		t.Errorf("expected namespace %q, got %q", "test-ns", job.Namespace)
	}

	// Verify the helm install command contains the chart version
	containers := job.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	args := strings.Join(containers[0].Args, " ")
	if !strings.Contains(args, "helm upgrade --install") {
		t.Error("expected helm install command in job args")
	}
	if !strings.Contains(args, "--version 0.73.1") {
		t.Error("expected chart version 0.73.1 in job args")
	}

	// Verify values volume mount
	if len(containers[0].VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(containers[0].VolumeMounts))
	}
	if containers[0].VolumeMounts[0].MountPath != "/values" {
		t.Errorf("expected volume mount at /values, got %q", containers[0].VolumeMounts[0].MountPath)
	}
}

func TestGetTemporalFrontendURL(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		expected  string
	}{
		{
			name:      "custom namespace",
			namespace: "my-ns",
			expected:  "temporal-frontend.my-ns.svc:7233",
		},
		{
			name:      "empty namespace uses default",
			namespace: "",
			expected:  "temporal-frontend.temporal.svc:7233",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetTemporalFrontendURL(tt.namespace)
			if result != tt.expected {
				t.Errorf("GetTemporalFrontendURL(%q) = %q, want %q", tt.namespace, result, tt.expected)
			}
		})
	}
}

// --- Keycloak tests ---

func TestBuildKeycloakInstance(t *testing.T) {
	deployment := newTestDeployment()

	kc, err := BuildKeycloakInstance(deployment, "test-ns")
	if err != nil {
		t.Fatalf("BuildKeycloakInstance returned error: %v", err)
	}

	// Verify name is "keycloak" (not "carbide-keycloak")
	if kc.GetName() != "keycloak" {
		t.Errorf("expected name %q, got %q", "keycloak", kc.GetName())
	}

	// Verify kind
	if kc.GetKind() != "Keycloak" {
		t.Errorf("expected kind %q, got %q", "Keycloak", kc.GetKind())
	}

	// Verify apiVersion
	if kc.GetAPIVersion() != "k8s.keycloak.org/v2alpha1" {
		t.Errorf("expected apiVersion %q, got %q", "k8s.keycloak.org/v2alpha1", kc.GetAPIVersion())
	}

	// Verify namespace
	if kc.GetNamespace() != "test-ns" {
		t.Errorf("expected namespace %q, got %q", "test-ns", kc.GetNamespace())
	}
}

func TestBuildKeycloakRealmImport(t *testing.T) {
	deployment := newTestDeployment()
	deployment.Spec.Rest.Keycloak.Realm = "carbide"

	ri, err := BuildKeycloakRealmImport(deployment, "test-ns")
	if err != nil {
		t.Fatalf("BuildKeycloakRealmImport returned error: %v", err)
	}

	// Verify kind
	if ri.GetKind() != "KeycloakRealmImport" {
		t.Errorf("expected kind %q, got %q", "KeycloakRealmImport", ri.GetKind())
	}

	// Verify name includes realm
	expectedName := "keycloak-carbide"
	if ri.GetName() != expectedName {
		t.Errorf("expected name %q, got %q", expectedName, ri.GetName())
	}

	// Verify the realm spec contains the correct realm name
	spec := ri.Object["spec"].(map[string]interface{})
	realm := spec["realm"].(map[string]interface{})
	if realm["realm"] != "carbide" {
		t.Errorf("expected realm name %q, got %v", "carbide", realm["realm"])
	}
	if realm["id"] != "carbide" {
		t.Errorf("expected realm id %q, got %v", "carbide", realm["id"])
	}
}

// --- Auth config tests ---

func TestBuildRestAPIAuthConfig(t *testing.T) {
	tests := []struct {
		name     string
		modify   func(*carbitev1alpha1.CarbideDeployment)
		contains []string
		exact    string
	}{
		{
			name: "managed mode generates keycloak auth",
			modify: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Rest.Keycloak.Mode = carbitev1alpha1.AuthModeManaged
				d.Spec.Rest.Keycloak.Realm = "carbide"
			},
			contains: []string{
				"auth:",
				"name: keycloak",
				"origin: 2",
				"/realms/carbide/protocol/openid-connect/certs",
			},
		},
		{
			name: "external mode generates authProviders",
			modify: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Rest.Keycloak.Mode = carbitev1alpha1.AuthModeExternal
				d.Spec.Rest.Keycloak.AuthProviders = []carbitev1alpha1.AuthProviderConfig{
					{
						Name:    "my-provider",
						JWKSURL: "https://example.com/.well-known/jwks.json",
					},
				}
			},
			contains: []string{
				"auth:",
				"name: my-provider",
				"url: https://example.com/.well-known/jwks.json",
			},
		},
		{
			name: "disabled mode returns auth disabled comment",
			modify: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Rest.Keycloak.Mode = carbitev1alpha1.AuthModeDisabled
			},
			exact: "# auth disabled\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := newTestDeployment()
			tt.modify(deployment)

			result := BuildRestAPIAuthConfig(deployment, "test-ns")

			if tt.exact != "" {
				if result != tt.exact {
					t.Errorf("expected exact %q, got %q", tt.exact, result)
				}
				return
			}

			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("expected result to contain %q, got:\n%s", s, result)
				}
			}
		})
	}
}

// --- REST API tests ---

func TestBuildRestAPIDeployment(t *testing.T) {
	deployment := newTestDeployment()

	dep := BuildRestAPIDeployment(deployment, "test-ns")

	// Verify name
	if dep.Name != "carbide-rest-api" {
		t.Errorf("expected name %q, got %q", "carbide-rest-api", dep.Name)
	}

	// Verify namespace
	if dep.Namespace != "test-ns" {
		t.Errorf("expected namespace %q, got %q", "test-ns", dep.Namespace)
	}

	// Verify ports
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	if len(containers[0].Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(containers[0].Ports))
	}
	if containers[0].Ports[0].ContainerPort != 8080 {
		t.Errorf("expected port 8080, got %d", containers[0].Ports[0].ContainerPort)
	}
	if containers[0].Ports[0].Name != "http" {
		t.Errorf("expected port name %q, got %q", "http", containers[0].Ports[0].Name)
	}

	// Verify config volume mount
	hasConfigMount := false
	for _, vm := range containers[0].VolumeMounts {
		if vm.Name == "config" && vm.MountPath == "/app/config.yaml" {
			hasConfigMount = true
			break
		}
	}
	if !hasConfigMount {
		t.Error("expected config volume mount at /app/config.yaml")
	}
}

func TestBuildRestAPIService(t *testing.T) {
	t.Run("ClusterIP by default", func(t *testing.T) {
		deployment := newTestDeployment()

		svc := BuildRestAPIService(deployment, "test-ns")

		// Verify name
		if svc.Name != "carbide-rest-api" {
			t.Errorf("expected name %q, got %q", "carbide-rest-api", svc.Name)
		}

		// Verify selector
		if svc.Spec.Selector["app"] != "carbide-rest-api" {
			t.Errorf("expected selector app=%q, got %q", "carbide-rest-api", svc.Spec.Selector["app"])
		}

		// Verify default type is ClusterIP
		if svc.Spec.Type != corev1.ServiceTypeClusterIP {
			t.Errorf("expected service type ClusterIP, got %v", svc.Spec.Type)
		}
	})

	t.Run("NodePort when configured", func(t *testing.T) {
		deployment := newTestDeployment()
		deployment.Spec.Rest.RestAPI.NodePort = 30080

		svc := BuildRestAPIService(deployment, "test-ns")

		if svc.Spec.Type != corev1.ServiceTypeNodePort {
			t.Errorf("expected service type NodePort, got %v", svc.Spec.Type)
		}
		if svc.Spec.Ports[0].NodePort != 30080 {
			t.Errorf("expected NodePort 30080, got %d", svc.Spec.Ports[0].NodePort)
		}
	})
}

// --- Worker tests ---

func TestBuildWorkflowConfigMap(t *testing.T) {
	deployment := newTestDeployment()

	cm := BuildWorkflowConfigMap(deployment, "test-ns")

	if cm.Name != "carbide-rest-workflow-config" {
		t.Errorf("expected name %q, got %q", "carbide-rest-workflow-config", cm.Name)
	}

	if _, ok := cm.Data["config.yaml"]; !ok {
		t.Error("expected config.yaml key in ConfigMap data")
	}
}

func TestBuildCloudWorkerDeployment(t *testing.T) {
	deployment := newTestDeployment()

	dep := BuildCloudWorkerDeployment(deployment, "test-ns")

	if dep.Name != "carbide-rest-cloud-worker" {
		t.Errorf("expected name %q, got %q", "carbide-rest-cloud-worker", dep.Name)
	}

	if dep.Namespace != "test-ns" {
		t.Errorf("expected namespace %q, got %q", "test-ns", dep.Namespace)
	}
}

func TestBuildSiteWorkerDeployment(t *testing.T) {
	deployment := newTestDeployment()

	dep := BuildSiteWorkerDeployment(deployment, "test-ns")

	if dep.Name != "carbide-rest-site-worker" {
		t.Errorf("expected name %q, got %q", "carbide-rest-site-worker", dep.Name)
	}

	if dep.Namespace != "test-ns" {
		t.Errorf("expected namespace %q, got %q", "test-ns", dep.Namespace)
	}
}

func TestWorkerConstants(t *testing.T) {
	if CloudWorkerName != "carbide-rest-cloud-worker" {
		t.Errorf("expected CloudWorkerName %q, got %q", "carbide-rest-cloud-worker", CloudWorkerName)
	}
	if SiteWorkerName != "carbide-rest-site-worker" {
		t.Errorf("expected SiteWorkerName %q, got %q", "carbide-rest-site-worker", SiteWorkerName)
	}
	if WorkflowCMName != "carbide-rest-workflow-config" {
		t.Errorf("expected WorkflowCMName %q, got %q", "carbide-rest-workflow-config", WorkflowCMName)
	}
}

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
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	testNamespace = "carbide-test"
	testPGHost    = "pg.carbide-test.svc"
	testPGPort    = int32(5432)
	testPGPass    = "s3cret"
	testVersion   = "v1.2.3"
)

// newTestDeployment returns a minimal CarbideDeployment suitable for most
// builder tests. Callers may mutate the returned object before passing it to
// a builder.
func newTestDeployment() *carbitev1alpha1.CarbideDeployment {
	return &carbitev1alpha1.CarbideDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: testNamespace,
			UID:       "test-uid-1234",
		},
		Spec: carbitev1alpha1.CarbideDeploymentSpec{
			Profile: carbitev1alpha1.ProfileSite,
			Version: testVersion,
			Network: carbitev1alpha1.NetworkConfig{
				Domain:           "example.local",
				Interface:        "eth0",
				IP:               "10.0.0.1",
				AdminNetworkCIDR: "10.0.0.0/24",
			},
			Core: carbitev1alpha1.CoreConfig{
				API:  carbitev1alpha1.APIConfig{},
				DHCP: carbitev1alpha1.DHCPConfig{Enabled: true},
				PXE:  carbitev1alpha1.PXEConfig{Enabled: true},
				DNS:  carbitev1alpha1.DNSConfig{Enabled: true},
			},
		},
	}
}

// --- BuildAPIConfigMap ---

func TestBuildAPIConfigMap_DefaultPort(t *testing.T) {
	dep := newTestDeployment()
	cm := BuildAPIConfigMap(dep, testNamespace, testPGHost, testPGPort)

	toml := cm.Data["carbide-api-config.toml"]
	if !strings.Contains(toml, `listen = "[::]:1079"`) {
		t.Errorf("expected default port 1079 in listen, got:\n%s", toml)
	}
	if !strings.Contains(toml, `metrics_endpoint = "[::]:1080"`) {
		t.Errorf("expected metrics port 1080, got:\n%s", toml)
	}
}

func TestBuildAPIConfigMap_TrustDomain(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.TLS = &carbitev1alpha1.TLSConfig{
		SPIFFE: &carbitev1alpha1.SPIFFEConfig{
			TrustDomain: "custom.trust",
		},
	}

	cm := BuildAPIConfigMap(dep, testNamespace, testPGHost, testPGPort)
	toml := cm.Data["carbide-api-config.toml"]

	if !strings.Contains(toml, `spiffe_trust_domain = "custom.trust"`) {
		t.Errorf("expected custom trust domain, got:\n%s", toml)
	}
}

func TestBuildAPIConfigMap_TLSPaths(t *testing.T) {
	dep := newTestDeployment()
	cm := BuildAPIConfigMap(dep, testNamespace, testPGHost, testPGPort)

	toml := cm.Data["carbide-api-config.toml"]
	certDir := tls.CertDir

	for _, expected := range []string{
		fmt.Sprintf(`root_cafile_path = "%s/ca.crt"`, certDir),
		fmt.Sprintf(`identity_pemfile_path = "%s/tls.crt"`, certDir),
		fmt.Sprintf(`identity_keyfile_path = "%s/tls.key"`, certDir),
	} {
		if !strings.Contains(toml, expected) {
			t.Errorf("expected TLS path %q in TOML, got:\n%s", expected, toml)
		}
	}
}

func TestBuildAPIConfigMap_EnvStyleData(t *testing.T) {
	dep := newTestDeployment()
	cm := BuildAPIConfigMap(dep, testNamespace, testPGHost, testPGPort)

	checks := map[string]string{
		"CARBIDE_DOMAIN": "example.local",
		"POSTGRES_HOST":  testPGHost,
		"POSTGRES_PORT":  fmt.Sprintf("%d", testPGPort),
		"POSTGRES_DB":    "carbide",
	}
	for k, want := range checks {
		if got := cm.Data[k]; got != want {
			t.Errorf("Data[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestBuildAPIConfigMap_NetworkConfig(t *testing.T) {
	tests := []struct {
		name            string
		profile         carbitev1alpha1.DeploymentProfile
		expectInterface bool
	}{
		{
			name:            "site profile includes network config",
			profile:         carbitev1alpha1.ProfileSite,
			expectInterface: true,
		},
		{
			name:            "management-with-site profile includes network config",
			profile:         carbitev1alpha1.ProfileManagementWithSite,
			expectInterface: true,
		},
		{
			name:            "management profile excludes network config",
			profile:         carbitev1alpha1.ProfileManagement,
			expectInterface: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := newTestDeployment()
			dep.Spec.Profile = tt.profile

			cm := BuildAPIConfigMap(dep, testNamespace, testPGHost, testPGPort)

			_, hasInterface := cm.Data["CARBIDE_NETWORK_INTERFACE"]
			_, hasIP := cm.Data["CARBIDE_NETWORK_IP"]
			_, hasCIDR := cm.Data["CARBIDE_NETWORK_CIDR"]

			if tt.expectInterface {
				if !hasInterface || !hasIP || !hasCIDR {
					t.Errorf("expected network config keys to be present for profile %q", tt.profile)
				}
			} else {
				if hasInterface || hasIP || hasCIDR {
					t.Errorf("expected no network config keys for profile %q", tt.profile)
				}
			}
		})
	}
}

// --- BuildAPIDeployment ---

func TestBuildAPIDeployment_BasicFields(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildAPIDeployment(dep, testNamespace)

	if deploy.Name != "carbide-api" {
		t.Errorf("Name = %q, want %q", deploy.Name, "carbide-api")
	}
	if deploy.Namespace != testNamespace {
		t.Errorf("Namespace = %q, want %q", deploy.Namespace, testNamespace)
	}
	if deploy.Spec.Replicas == nil || *deploy.Spec.Replicas != 1 {
		t.Errorf("Replicas = %v, want 1", deploy.Spec.Replicas)
	}
}

func TestBuildAPIDeployment_ContainerPort(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildAPIDeployment(dep, testNamespace)

	container := deploy.Spec.Template.Spec.Containers[0]
	if len(container.Ports) == 0 {
		t.Fatal("expected at least one container port")
	}
	if container.Ports[0].ContainerPort != 1079 {
		t.Errorf("ContainerPort = %d, want 1079", container.Ports[0].ContainerPort)
	}
}

func TestBuildAPIDeployment_Image(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildAPIDeployment(dep, testNamespace)

	container := deploy.Spec.Template.Spec.Containers[0]
	expected := fmt.Sprintf("ghcr.io/nvidia/carbide-core:%s", testVersion)
	if container.Image != expected {
		t.Errorf("Image = %q, want %q", container.Image, expected)
	}
}

func TestBuildAPIDeployment_InitContainer(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildAPIDeployment(dep, testNamespace)

	initContainers := deploy.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("expected 1 init container, got %d", len(initContainers))
	}
	if initContainers[0].Name != "db-migrations" {
		t.Errorf("init container name = %q, want %q", initContainers[0].Name, "db-migrations")
	}
}

func TestBuildAPIDeployment_OwnerReference(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildAPIDeployment(dep, testNamespace)

	assertOwnerReference(t, deploy.OwnerReferences)
}

// --- BuildAPISecret ---

func TestBuildAPISecret_DatabaseURL(t *testing.T) {
	dep := newTestDeployment()
	secret := BuildAPISecret(dep, testNamespace, testPGHost, testPGPort, testPGPass)

	dbURL := secret.StringData["database-url"]
	expected := fmt.Sprintf("postgres://carbide:%s@%s:%d/carbide?sslmode=require", testPGPass, testPGHost, testPGPort)
	if dbURL != expected {
		t.Errorf("database-url = %q, want %q", dbURL, expected)
	}
}

func TestBuildAPISecret_UsesCarbideUserAndDB(t *testing.T) {
	dep := newTestDeployment()
	secret := BuildAPISecret(dep, testNamespace, testPGHost, testPGPort, testPGPass)

	dbURL := secret.StringData["database-url"]
	if !strings.Contains(dbURL, "postgres://carbide:") {
		t.Errorf("expected carbide user in database URL, got %q", dbURL)
	}
	if !strings.Contains(dbURL, "/carbide?") {
		t.Errorf("expected carbide database in database URL, got %q", dbURL)
	}
}

// --- BuildAPIService ---

func TestBuildAPIService_SelectorAndPorts(t *testing.T) {
	dep := newTestDeployment()
	svc := BuildAPIService(dep, testNamespace)

	if svc.Spec.Selector["app"] != "carbide-api" {
		t.Errorf("selector app = %q, want %q", svc.Spec.Selector["app"], "carbide-api")
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(svc.Spec.Ports))
	}
	port := svc.Spec.Ports[0]
	if port.Name != "grpc" {
		t.Errorf("port name = %q, want %q", port.Name, "grpc")
	}
	if port.Port != 1079 {
		t.Errorf("port = %d, want 1079", port.Port)
	}
	if port.Protocol != corev1.ProtocolTCP {
		t.Errorf("protocol = %v, want TCP", port.Protocol)
	}
}

// --- BuildRLADeployment ---

func TestBuildRLADeployment_NilWhenNoConfig(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.RLA = nil

	deploy := BuildRLADeployment(dep, testNamespace)
	if deploy != nil {
		t.Error("expected nil when RLA config is nil")
	}
}

func TestBuildRLADeployment_Command(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: true}

	deploy := BuildRLADeployment(dep, testNamespace)
	container := deploy.Spec.Template.Spec.Containers[0]

	fullCmd := strings.Join(append(container.Command, container.Args...), " ")
	expected := "/app/rla serve --port 50051"
	if fullCmd != expected {
		t.Errorf("command = %q, want %q", fullCmd, expected)
	}
}

func TestBuildRLADeployment_DBEnvVars(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: true}

	deploy := BuildRLADeployment(dep, testNamespace)
	container := deploy.Spec.Template.Spec.Containers[0]

	secretName := "carbide-postgres-pguser-rla"
	expectedEnvSecrets := map[string]string{
		"DB_ADDR":     "host",
		"DB_PORT":     "port",
		"DB_USER":     "user",
		"DB_PASSWORD": "password",
		"DB_DATABASE": "dbname",
	}

	envMap := envToMap(container.Env)
	for envName, secretKey := range expectedEnvSecrets {
		env, ok := envMap[envName]
		if !ok {
			t.Errorf("missing env var %q", envName)
			continue
		}
		if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
			t.Errorf("env %q does not reference a secret", envName)
			continue
		}
		if env.ValueFrom.SecretKeyRef.Name != secretName {
			t.Errorf("env %q secret name = %q, want %q", envName, env.ValueFrom.SecretKeyRef.Name, secretName)
		}
		if env.ValueFrom.SecretKeyRef.Key != secretKey {
			t.Errorf("env %q secret key = %q, want %q", envName, env.ValueFrom.SecretKeyRef.Key, secretKey)
		}
	}
}

func TestBuildRLADeployment_ServiceEnvVars(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: true}

	deploy := BuildRLADeployment(dep, testNamespace)
	container := deploy.Spec.Template.Spec.Containers[0]
	envMap := envToMap(container.Env)

	checks := map[string]string{
		"TEMPORAL_HOST":      fmt.Sprintf("temporal-frontend.%s.svc", testNamespace),
		"TEMPORAL_NAMESPACE": "rla",
		"PSM_API_URL":        "carbide-psm:50051",
		"CERTDIR":            tls.CertDir,
	}
	for name, want := range checks {
		env, ok := envMap[name]
		if !ok {
			t.Errorf("missing env var %q", name)
			continue
		}
		if env.Value != want {
			t.Errorf("env %q = %q, want %q", name, env.Value, want)
		}
	}
}

func TestBuildRLADeployment_DBCertsVolume(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: true}

	deploy := BuildRLADeployment(dep, testNamespace)

	found := false
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.Name == "db-certs" {
			found = true
			if v.Secret == nil {
				t.Error("db-certs volume has no secret source")
			}
			break
		}
	}
	if !found {
		t.Error("expected db-certs volume")
	}

	foundMount := false
	for _, vm := range deploy.Spec.Template.Spec.Containers[0].VolumeMounts {
		if vm.Name == "db-certs" {
			foundMount = true
			break
		}
	}
	if !foundMount {
		t.Error("expected db-certs volume mount on container")
	}
}

// --- BuildPSMDeployment ---

func TestBuildPSMDeployment_NilWhenNoConfig(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.PSM = nil

	deploy := BuildPSMDeployment(dep, testNamespace)
	if deploy != nil {
		t.Error("expected nil when PSM config is nil")
	}
}

func TestBuildPSMDeployment_Command(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: true}

	deploy := BuildPSMDeployment(dep, testNamespace)
	container := deploy.Spec.Template.Spec.Containers[0]

	fullCmd := strings.Join(append(container.Command, container.Args...), " ")
	expected := "/app/psm serve --port 50051 --datastore Persistent"
	if fullCmd != expected {
		t.Errorf("command = %q, want %q", fullCmd, expected)
	}
}

func TestBuildPSMDeployment_DBEnvVars(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: true}

	deploy := BuildPSMDeployment(dep, testNamespace)
	container := deploy.Spec.Template.Spec.Containers[0]

	secretName := "carbide-postgres-pguser-psm"
	expectedEnvSecrets := map[string]string{
		"DB_ADDR":     "host",
		"DB_PORT":     "port",
		"DB_USER":     "user",
		"DB_PASSWORD": "password",
		"DB_DATABASE": "dbname",
	}

	envMap := envToMap(container.Env)
	for envName, secretKey := range expectedEnvSecrets {
		env, ok := envMap[envName]
		if !ok {
			t.Errorf("missing env var %q", envName)
			continue
		}
		if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
			t.Errorf("env %q does not reference a secret", envName)
			continue
		}
		if env.ValueFrom.SecretKeyRef.Name != secretName {
			t.Errorf("env %q secret name = %q, want %q", envName, env.ValueFrom.SecretKeyRef.Name, secretName)
		}
		if env.ValueFrom.SecretKeyRef.Key != secretKey {
			t.Errorf("env %q secret key = %q, want %q", envName, env.ValueFrom.SecretKeyRef.Key, secretKey)
		}
	}
}

func TestBuildPSMDeployment_VaultManagedMode(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: true}
	dep.Spec.Core.Vault = &carbitev1alpha1.VaultConfig{
		Mode: carbitev1alpha1.ManagedMode,
	}

	deploy := BuildPSMDeployment(dep, testNamespace)
	container := deploy.Spec.Template.Spec.Containers[0]
	envMap := envToMap(container.Env)

	vaultAddr, ok := envMap["VAULT_ADDR"]
	if !ok {
		t.Fatal("missing VAULT_ADDR env var")
	}
	expected := fmt.Sprintf("http://vault.%s.svc:8200", testNamespace)
	if vaultAddr.Value != expected {
		t.Errorf("VAULT_ADDR = %q, want %q", vaultAddr.Value, expected)
	}

	vaultToken, ok := envMap["VAULT_TOKEN"]
	if !ok {
		t.Fatal("missing VAULT_TOKEN env var")
	}
	if vaultToken.ValueFrom == nil || vaultToken.ValueFrom.SecretKeyRef == nil {
		t.Fatal("VAULT_TOKEN does not reference a secret")
	}
	if vaultToken.ValueFrom.SecretKeyRef.Name != "vault-unseal-secret" {
		t.Errorf("VAULT_TOKEN secret name = %q, want %q", vaultToken.ValueFrom.SecretKeyRef.Name, "vault-unseal-secret")
	}
	if vaultToken.ValueFrom.SecretKeyRef.Key != "root-token" {
		t.Errorf("VAULT_TOKEN secret key = %q, want %q", vaultToken.ValueFrom.SecretKeyRef.Key, "root-token")
	}
}

func TestBuildPSMDeployment_DBCertsVolume(t *testing.T) {
	dep := newTestDeployment()
	dep.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: true}

	deploy := BuildPSMDeployment(dep, testNamespace)

	found := false
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.Name == "db-certs" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected db-certs volume")
	}
}

// --- BuildCasbinPolicyConfigMap ---

func TestBuildCasbinPolicyConfigMap(t *testing.T) {
	tests := []struct {
		name       string
		rla        *carbitev1alpha1.RLAConfig
		psm        *carbitev1alpha1.PSMConfig
		expectRLA  bool
		expectPSM  bool
		expectDHCP bool
		expectDNS  bool
	}{
		{
			name:       "base rules only",
			rla:        nil,
			psm:        nil,
			expectRLA:  false,
			expectPSM:  false,
			expectDHCP: true,
			expectDNS:  true,
		},
		{
			name:       "RLA enabled adds RLA rules",
			rla:        &carbitev1alpha1.RLAConfig{Enabled: true},
			psm:        nil,
			expectRLA:  true,
			expectPSM:  false,
			expectDHCP: true,
			expectDNS:  true,
		},
		{
			name:       "PSM enabled adds PSM rules",
			rla:        nil,
			psm:        &carbitev1alpha1.PSMConfig{Enabled: true},
			expectRLA:  false,
			expectPSM:  true,
			expectDHCP: true,
			expectDNS:  true,
		},
		{
			name:       "both RLA and PSM enabled",
			rla:        &carbitev1alpha1.RLAConfig{Enabled: true},
			psm:        &carbitev1alpha1.PSMConfig{Enabled: true},
			expectRLA:  true,
			expectPSM:  true,
			expectDHCP: true,
			expectDNS:  true,
		},
		{
			name:       "RLA present but disabled",
			rla:        &carbitev1alpha1.RLAConfig{Enabled: false},
			psm:        nil,
			expectRLA:  false,
			expectPSM:  false,
			expectDHCP: true,
			expectDNS:  true,
		},
		{
			name:       "PSM present but disabled",
			rla:        nil,
			psm:        &carbitev1alpha1.PSMConfig{Enabled: false},
			expectRLA:  false,
			expectPSM:  false,
			expectDHCP: true,
			expectDNS:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := newTestDeployment()
			dep.Spec.Core.RLA = tt.rla
			dep.Spec.Core.PSM = tt.psm

			cm := BuildCasbinPolicyConfigMap(dep, testNamespace)
			policy := cm.Data["casbin-policy.csv"]

			assertContains(t, policy, "carbide-dhcp", tt.expectDHCP)
			assertContains(t, policy, "carbide-dns", tt.expectDNS)
			assertContains(t, policy, "carbide-rla", tt.expectRLA)
			assertContains(t, policy, "carbide-psm", tt.expectPSM)
		})
	}
}

// --- BuildPXEDeployment ---

func TestBuildPXEDeployment_IsDeployment(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildPXEDeployment(dep, testNamespace)

	// Verify the returned type is *appsv1.Deployment, not a DaemonSet.
	var _ *appsv1.Deployment = deploy
	if deploy == nil {
		t.Fatal("expected non-nil Deployment")
	}
}

func TestBuildPXEDeployment_Command(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildPXEDeployment(dep, testNamespace)

	container := deploy.Spec.Template.Spec.Containers[0]
	fullCmd := strings.Join(append(container.Command, container.Args...), " ")
	expected := "/opt/carbide/carbide -s /forge-boot-artifacts"
	if fullCmd != expected {
		t.Errorf("command = %q, want %q", fullCmd, expected)
	}
}

func TestBuildPXEDeployment_Port(t *testing.T) {
	dep := newTestDeployment()
	deploy := BuildPXEDeployment(dep, testNamespace)

	container := deploy.Spec.Template.Spec.Containers[0]
	if len(container.Ports) == 0 {
		t.Fatal("expected at least one container port")
	}
	if container.Ports[0].ContainerPort != 8080 {
		t.Errorf("ContainerPort = %d, want 8080", container.Ports[0].ContainerPort)
	}
}

// --- BuildServiceAccount ---

func TestBuildServiceAccount(t *testing.T) {
	dep := newTestDeployment()
	sa := BuildServiceAccount("carbide-api", testNamespace, dep)

	if sa.Name != "carbide-api" {
		t.Errorf("Name = %q, want %q", sa.Name, "carbide-api")
	}
	if sa.Namespace != testNamespace {
		t.Errorf("Namespace = %q, want %q", sa.Namespace, testNamespace)
	}
	if sa.Labels == nil {
		t.Fatal("expected labels to be set")
	}
	if sa.Labels["app.kubernetes.io/managed-by"] != "carbide-operator" {
		t.Errorf("managed-by label = %q, want %q", sa.Labels["app.kubernetes.io/managed-by"], "carbide-operator")
	}
	if sa.AutomountServiceAccountToken == nil || !*sa.AutomountServiceAccountToken {
		t.Error("expected AutomountServiceAccountToken to be true")
	}

	assertOwnerReference(t, sa.OwnerReferences)
}

// --- helpers ---

// envToMap converts a slice of EnvVar to a map keyed by Name.
func envToMap(envs []corev1.EnvVar) map[string]corev1.EnvVar {
	m := make(map[string]corev1.EnvVar, len(envs))
	for _, e := range envs {
		m[e.Name] = e
	}
	return m
}

// assertContains checks that the haystack contains (or does not contain) the needle.
func assertContains(t *testing.T, haystack, needle string, shouldContain bool) {
	t.Helper()
	contains := strings.Contains(haystack, needle)
	if shouldContain && !contains {
		t.Errorf("expected policy to contain %q, but it did not.\npolicy:\n%s", needle, haystack)
	}
	if !shouldContain && contains {
		t.Errorf("expected policy NOT to contain %q, but it did.\npolicy:\n%s", needle, haystack)
	}
}

// assertOwnerReference verifies the owner reference uses Kind=CarbideDeployment
// with group=carbide.nvidia.com.
func assertOwnerReference(t *testing.T, refs []metav1.OwnerReference) {
	t.Helper()
	if len(refs) == 0 {
		t.Fatal("expected at least one owner reference")
	}
	ref := refs[0]
	if ref.Kind != "CarbideDeployment" {
		t.Errorf("OwnerReference Kind = %q, want %q", ref.Kind, "CarbideDeployment")
	}
	if !strings.Contains(ref.APIVersion, "carbide.nvidia.com") {
		t.Errorf("OwnerReference APIVersion = %q, want it to contain %q", ref.APIVersion, "carbide.nvidia.com")
	}
}

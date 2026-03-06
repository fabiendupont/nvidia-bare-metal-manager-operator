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

package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newCarbideDeployment creates a minimal CarbideDeployment for testing.
func newCarbideDeployment(name string, profile DeploymentProfile) *CarbideDeployment {
	cd := &CarbideDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: CarbideDeploymentSpec{
			Profile: profile,
			Version: "latest",
			Core:    CoreConfig{},
			Infrastructure: &InfrastructureConfig{
				PostgreSQL: PostgreSQLConfig{},
			},
		},
	}
	if profile == ProfileManagement || profile == ProfileManagementWithSite {
		cd.Spec.Rest = &RestConfig{}
	}
	return cd
}

// --- Default() tests ---

func TestDefault_ManagementProfile_Databases(t *testing.T) {
	cd := newCarbideDeployment("test-mgmt", ProfileManagement)
	cd.applyDefaults()

	expected := []string{"forge", "temporal", "temporal_visibility", "keycloak"}
	if len(cd.Spec.Infrastructure.PostgreSQL.Databases) != len(expected) {
		t.Fatalf("expected %d databases, got %d", len(expected), len(cd.Spec.Infrastructure.PostgreSQL.Databases))
	}
	for i, db := range expected {
		if cd.Spec.Infrastructure.PostgreSQL.Databases[i] != db {
			t.Errorf("expected database[%d] = %q, got %q", i, db, cd.Spec.Infrastructure.PostgreSQL.Databases[i])
		}
	}
}

func TestDefault_ManagementProfile_DisablesDHCPPXEDNS(t *testing.T) {
	cd := newCarbideDeployment("test-mgmt", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Core.DHCP.Enabled {
		t.Error("expected DHCP disabled for management profile")
	}
	if cd.Spec.Core.PXE.Enabled {
		t.Error("expected PXE disabled for management profile")
	}
	if cd.Spec.Core.DNS.Enabled {
		t.Error("expected DNS disabled for management profile")
	}
}

func TestDefault_ManagementProfile_EnablesREST(t *testing.T) {
	cd := newCarbideDeployment("test-mgmt", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest == nil {
		t.Fatal("expected Rest to be non-nil")
	}
	if !cd.Spec.Rest.Enabled {
		t.Error("expected REST enabled for management profile")
	}
}

func TestDefault_SiteProfile_Databases(t *testing.T) {
	cd := newCarbideDeployment("test-site", ProfileSite)
	cd.applyDefaults()

	expected := []string{"carbide", "forge", "rla", "psm"}
	if len(cd.Spec.Infrastructure.PostgreSQL.Databases) != len(expected) {
		t.Fatalf("expected %d databases, got %d", len(expected), len(cd.Spec.Infrastructure.PostgreSQL.Databases))
	}
	for i, db := range expected {
		if cd.Spec.Infrastructure.PostgreSQL.Databases[i] != db {
			t.Errorf("expected database[%d] = %q, got %q", i, db, cd.Spec.Infrastructure.PostgreSQL.Databases[i])
		}
	}
}

func TestDefault_SiteProfile_EnablesDHCPPXEDNS(t *testing.T) {
	cd := newCarbideDeployment("test-site", ProfileSite)
	cd.applyDefaults()

	if !cd.Spec.Core.DHCP.Enabled {
		t.Error("expected DHCP enabled for site profile")
	}
	if !cd.Spec.Core.PXE.Enabled {
		t.Error("expected PXE enabled for site profile")
	}
	if !cd.Spec.Core.DNS.Enabled {
		t.Error("expected DNS enabled for site profile")
	}
}

func TestDefault_SiteProfile_DisablesREST(t *testing.T) {
	cd := newCarbideDeployment("test-site", ProfileSite)
	cd.Spec.Rest = &RestConfig{}
	cd.applyDefaults()

	if cd.Spec.Rest.Enabled {
		t.Error("expected REST disabled for site profile")
	}
}

func TestDefault_ManagementWithSiteProfile_Databases(t *testing.T) {
	cd := newCarbideDeployment("test-combo", ProfileManagementWithSite)
	cd.applyDefaults()

	expected := []string{"carbide", "forge", "rla", "psm", "temporal", "temporal_visibility", "keycloak"}
	if len(cd.Spec.Infrastructure.PostgreSQL.Databases) != len(expected) {
		t.Fatalf("expected %d databases, got %d", len(expected), len(cd.Spec.Infrastructure.PostgreSQL.Databases))
	}
	for i, db := range expected {
		if cd.Spec.Infrastructure.PostgreSQL.Databases[i] != db {
			t.Errorf("expected database[%d] = %q, got %q", i, db, cd.Spec.Infrastructure.PostgreSQL.Databases[i])
		}
	}
}

func TestDefault_ManagementWithSiteProfile_EnablesEverything(t *testing.T) {
	cd := newCarbideDeployment("test-combo", ProfileManagementWithSite)
	cd.applyDefaults()

	if !cd.Spec.Core.DHCP.Enabled {
		t.Error("expected DHCP enabled for management-with-site profile")
	}
	if !cd.Spec.Core.PXE.Enabled {
		t.Error("expected PXE enabled for management-with-site profile")
	}
	if !cd.Spec.Core.DNS.Enabled {
		t.Error("expected DNS enabled for management-with-site profile")
	}
	if cd.Spec.Rest == nil || !cd.Spec.Rest.Enabled {
		t.Error("expected REST enabled for management-with-site profile")
	}
}

func TestDefault_TLS_NilCreatesSPIFFE(t *testing.T) {
	cd := newCarbideDeployment("test-tls", ProfileManagement)
	cd.Spec.TLS = nil
	cd.applyDefaults()

	if cd.Spec.TLS == nil {
		t.Fatal("expected TLS to be set")
	}
	if cd.Spec.TLS.Mode != TLSModeSpiffe {
		t.Errorf("expected TLS mode %q, got %q", TLSModeSpiffe, cd.Spec.TLS.Mode)
	}
	if cd.Spec.TLS.SPIFFE == nil {
		t.Fatal("expected SPIFFE config to be set")
	}
	if cd.Spec.TLS.SPIFFE.TrustDomain != "carbide.local" {
		t.Errorf("expected trust domain %q, got %q", "carbide.local", cd.Spec.TLS.SPIFFE.TrustDomain)
	}
	if cd.Spec.TLS.SPIFFE.HelperImage != "ghcr.io/nvidia/spiffe-helper:latest" {
		t.Errorf("expected helper image %q, got %q", "ghcr.io/nvidia/spiffe-helper:latest", cd.Spec.TLS.SPIFFE.HelperImage)
	}
	if cd.Spec.TLS.SPIFFE.ClassName != "zero-trust-workload-identity-manager-spire" {
		t.Errorf("expected class name %q, got %q", "zero-trust-workload-identity-manager-spire", cd.Spec.TLS.SPIFFE.ClassName)
	}
}

func TestDefault_TLS_ExistingEmptyModeSetsSpiffe(t *testing.T) {
	cd := newCarbideDeployment("test-tls", ProfileManagement)
	cd.Spec.TLS = &TLSConfig{}
	cd.applyDefaults()

	if cd.Spec.TLS.Mode != TLSModeSpiffe {
		t.Errorf("expected TLS mode %q, got %q", TLSModeSpiffe, cd.Spec.TLS.Mode)
	}
	if cd.Spec.TLS.SPIFFE == nil {
		t.Fatal("expected SPIFFE config to be created")
	}
}

func TestDefault_VaultCreatedForSiteProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile DeploymentProfile
		want    bool
	}{
		{"management", ProfileManagement, false},
		{"site", ProfileSite, true},
		{"management-with-site", ProfileManagementWithSite, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := newCarbideDeployment("test", tt.profile)
			cd.applyDefaults()

			if tt.want && cd.Spec.Core.Vault == nil {
				t.Error("expected Vault to be created")
			}
			if !tt.want && cd.Spec.Core.Vault != nil {
				t.Error("expected Vault to be nil")
			}
		})
	}
}

func TestDefault_VaultDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.applyDefaults()

	if cd.Spec.Core.Vault.Mode != ManagedMode {
		t.Errorf("expected vault mode %q, got %q", ManagedMode, cd.Spec.Core.Vault.Mode)
	}
	if cd.Spec.Core.Vault.Version != "1.15.6" {
		t.Errorf("expected vault version %q, got %q", "1.15.6", cd.Spec.Core.Vault.Version)
	}
	if cd.Spec.Core.Vault.KVMountPath != "secrets" {
		t.Errorf("expected vault KVMountPath %q, got %q", "secrets", cd.Spec.Core.Vault.KVMountPath)
	}
}

func TestDefault_RLACreatedForSiteProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile DeploymentProfile
		want    bool
	}{
		{"management", ProfileManagement, false},
		{"site", ProfileSite, true},
		{"management-with-site", ProfileManagementWithSite, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := newCarbideDeployment("test", tt.profile)
			cd.applyDefaults()

			if tt.want && cd.Spec.Core.RLA == nil {
				t.Error("expected RLA to be created")
			}
			if !tt.want && cd.Spec.Core.RLA != nil {
				t.Error("expected RLA to be nil")
			}
		})
	}
}

func TestDefault_PSMCreatedForSiteProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile DeploymentProfile
		want    bool
	}{
		{"management", ProfileManagement, false},
		{"site", ProfileSite, true},
		{"management-with-site", ProfileManagementWithSite, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := newCarbideDeployment("test", tt.profile)
			cd.applyDefaults()

			if tt.want && cd.Spec.Core.PSM == nil {
				t.Error("expected PSM to be created")
			}
			if !tt.want && cd.Spec.Core.PSM != nil {
				t.Error("expected PSM to be nil")
			}
		})
	}
}

func TestDefault_Namespace(t *testing.T) {
	tests := []struct {
		name    string
		cdName  string
		profile DeploymentProfile
		wantNS  string
	}{
		{"management", "test-mgmt", ProfileManagement, "nvidia-carbide-mgmt"},
		{"site", "mysite", ProfileSite, "carbide-site-mysite"},
		{"management-with-site", "test", ProfileManagementWithSite, "nvidia-carbide"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := newCarbideDeployment(tt.cdName, tt.profile)
			cd.applyDefaults()

			if cd.Spec.Infrastructure.Namespace != tt.wantNS {
				t.Errorf("expected namespace %q, got %q", tt.wantNS, cd.Spec.Infrastructure.Namespace)
			}
		})
	}
}

func TestDefault_NamespaceNotOverriddenWhenSet(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Infrastructure.Namespace = "custom-ns"
	cd.applyDefaults()

	if cd.Spec.Infrastructure.Namespace != "custom-ns" {
		t.Errorf("expected namespace to remain %q, got %q", "custom-ns", cd.Spec.Infrastructure.Namespace)
	}
}

func TestDefault_CoreNamespaceInheritsInfrastructure(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Core.Namespace != cd.Spec.Infrastructure.Namespace {
		t.Errorf("expected core namespace %q to match infrastructure %q", cd.Spec.Core.Namespace, cd.Spec.Infrastructure.Namespace)
	}
}

func TestDefault_RestNamespaceInheritsInfrastructure(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest.Namespace != cd.Spec.Infrastructure.Namespace {
		t.Errorf("expected rest namespace %q to match infrastructure %q", cd.Spec.Rest.Namespace, cd.Spec.Infrastructure.Namespace)
	}
}

func TestDefault_PortDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Core.API.Port != 1079 {
		t.Errorf("expected API port 1079, got %d", cd.Spec.Core.API.Port)
	}
	if cd.Spec.Core.DNS.Port != 53 {
		t.Errorf("expected DNS port 53, got %d", cd.Spec.Core.DNS.Port)
	}
	if cd.Spec.Rest.RestAPI.Port != 8080 {
		t.Errorf("expected REST API port 8080, got %d", cd.Spec.Rest.RestAPI.Port)
	}
}

func TestDefault_PXEPortDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.applyDefaults()

	if cd.Spec.Core.PXE.TFTPPort != 69 {
		t.Errorf("expected TFTP port 69, got %d", cd.Spec.Core.PXE.TFTPPort)
	}
	if cd.Spec.Core.PXE.HTTPPort != 8080 {
		t.Errorf("expected PXE HTTP port 8080, got %d", cd.Spec.Core.PXE.HTTPPort)
	}
}

func TestDefault_KeycloakRealmDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest.Keycloak.Realm != "carbide" {
		t.Errorf("expected keycloak realm %q, got %q", "carbide", cd.Spec.Rest.Keycloak.Realm)
	}
}

func TestDefault_TemporalChartVersionDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest.Temporal.ChartVersion != "0.73.1" {
		t.Errorf("expected temporal chart version %q, got %q", "0.73.1", cd.Spec.Rest.Temporal.ChartVersion)
	}
}

func TestDefault_TemporalVersionDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest.Temporal.Version != "1.22.0" {
		t.Errorf("expected temporal version %q, got %q", "1.22.0", cd.Spec.Rest.Temporal.Version)
	}
}

func TestDefault_PostgreSQLModeDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Infrastructure.PostgreSQL.Mode != ManagedMode {
		t.Errorf("expected postgresql mode %q, got %q", ManagedMode, cd.Spec.Infrastructure.PostgreSQL.Mode)
	}
}

func TestDefault_TemporalModeDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest.Temporal.Mode != ManagedMode {
		t.Errorf("expected temporal mode %q, got %q", ManagedMode, cd.Spec.Rest.Temporal.Mode)
	}
}

func TestDefault_KeycloakModeDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest.Keycloak.Mode != AuthModeManaged {
		t.Errorf("expected keycloak mode %q, got %q", AuthModeManaged, cd.Spec.Rest.Keycloak.Mode)
	}
}

func TestDefault_DomainDefault(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Network.Domain != "carbide.local" {
		t.Errorf("expected domain %q, got %q", "carbide.local", cd.Spec.Network.Domain)
	}
}

func TestDefault_ReplicaDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Core.API.Replicas != 1 {
		t.Errorf("expected API replicas 1, got %d", cd.Spec.Core.API.Replicas)
	}
	if cd.Spec.Infrastructure.PostgreSQL.Replicas != 1 {
		t.Errorf("expected PostgreSQL replicas 1, got %d", cd.Spec.Infrastructure.PostgreSQL.Replicas)
	}
	if cd.Spec.Rest.RestAPI.Replicas != 1 {
		t.Errorf("expected REST API replicas 1, got %d", cd.Spec.Rest.RestAPI.Replicas)
	}
	if cd.Spec.Rest.Temporal.Replicas != 1 {
		t.Errorf("expected Temporal replicas 1, got %d", cd.Spec.Rest.Temporal.Replicas)
	}
}

func TestDefault_RLADefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.applyDefaults()

	if cd.Spec.Core.RLA == nil {
		t.Fatal("expected RLA to be created")
	}
	if !cd.Spec.Core.RLA.Enabled {
		t.Error("expected RLA enabled")
	}
	if cd.Spec.Core.RLA.Port != 50051 {
		t.Errorf("expected RLA port 50051, got %d", cd.Spec.Core.RLA.Port)
	}
	if cd.Spec.Core.RLA.Replicas != 1 {
		t.Errorf("expected RLA replicas 1, got %d", cd.Spec.Core.RLA.Replicas)
	}
}

func TestDefault_PSMDefaults(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.applyDefaults()

	if cd.Spec.Core.PSM == nil {
		t.Fatal("expected PSM to be created")
	}
	if !cd.Spec.Core.PSM.Enabled {
		t.Error("expected PSM enabled")
	}
	if cd.Spec.Core.PSM.Port != 50051 {
		t.Errorf("expected PSM port 50051, got %d", cd.Spec.Core.PSM.Port)
	}
	if cd.Spec.Core.PSM.Replicas != 1 {
		t.Errorf("expected PSM replicas 1, got %d", cd.Spec.Core.PSM.Replicas)
	}
}

func TestDefault_DatabasesNotOverriddenWhenSet(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Infrastructure.PostgreSQL.Databases = []string{"custom_db"}
	cd.applyDefaults()

	if len(cd.Spec.Infrastructure.PostgreSQL.Databases) != 1 || cd.Spec.Infrastructure.PostgreSQL.Databases[0] != "custom_db" {
		t.Errorf("expected databases to remain [custom_db], got %v", cd.Spec.Infrastructure.PostgreSQL.Databases)
	}
}

func TestDefault_TemporalNamespaceDefault(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.applyDefaults()

	if cd.Spec.Rest.Temporal.Namespace != "temporal" {
		t.Errorf("expected temporal namespace %q, got %q", "temporal", cd.Spec.Rest.Temporal.Namespace)
	}
}

// --- Validation tests ---

func TestValidate_SiteProfile_RequiresNetworkInterface(t *testing.T) {
	tests := []struct {
		name    string
		profile DeploymentProfile
	}{
		{"site", ProfileSite},
		{"management-with-site", ProfileManagementWithSite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := newCarbideDeployment("test", tt.profile)
			cd.Spec.Network.IP = "10.0.0.1"
			cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"
			// Interface is empty
			_, err := cd.validateCarbideDeployment()
			if err == nil {
				t.Error("expected error for missing network.interface")
			}
		})
	}
}

func TestValidate_SiteProfile_RequiresNetworkIP(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"
	// IP is empty

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for missing network.ip")
	}
}

func TestValidate_SiteProfile_RequiresAdminNetworkCIDR(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.IP = "10.0.0.1"
	// AdminNetworkCIDR is empty

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for missing network.adminNetworkCIDR")
	}
}

func TestValidate_SiteProfile_ValidNetworkPasses(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.IP = "10.0.0.1"
	cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"

	_, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_ManagementProfile_RequiresRestConfig(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest = nil

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for missing rest config on management profile")
	}
}

func TestValidate_ManagementWithSiteProfile_RequiresRestConfig(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagementWithSite)
	cd.Spec.Rest = nil
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.IP = "10.0.0.1"
	cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for missing rest config on management-with-site profile")
	}
}

func TestValidate_ExternalPostgreSQL_RequiresConnection(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Infrastructure.PostgreSQL.Mode = ExternalMode
	cd.Spec.Infrastructure.PostgreSQL.Connection = nil

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for external postgresql without connection")
	}
}

func TestValidate_ExternalPostgreSQL_RequiresHost(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Infrastructure.PostgreSQL.Mode = ExternalMode
	cd.Spec.Infrastructure.PostgreSQL.Connection = &ExternalPGConnection{
		Host: "",
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for external postgresql without host")
	}
}

func TestValidate_ExternalPostgreSQL_NoUserSecretsWarning(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Infrastructure.PostgreSQL.Mode = ExternalMode
	cd.Spec.Infrastructure.PostgreSQL.Connection = &ExternalPGConnection{
		Host: "pg.example.com",
	}

	warnings, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	found := false
	for _, w := range warnings {
		if w == "infrastructure.postgresql.connection.userSecrets is recommended for external mode" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about missing userSecrets")
	}
}

func TestValidate_ExternalVault_RequiresAddress(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.IP = "10.0.0.1"
	cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"
	cd.Spec.Core.Vault = &VaultConfig{
		Mode:           ExternalMode,
		Address:        "",
		TokenSecretRef: &SecretRef{Name: "vault-token"},
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for external vault without address")
	}
}

func TestValidate_ExternalVault_RequiresTokenSecretRef(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.IP = "10.0.0.1"
	cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"
	cd.Spec.Core.Vault = &VaultConfig{
		Mode:    ExternalMode,
		Address: "https://vault.example.com",
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for external vault without tokenSecretRef")
	}
}

func TestValidate_ExternalKeycloak_RequiresAuthProviders(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest.Keycloak.Mode = AuthModeExternal
	cd.Spec.Rest.Keycloak.AuthProviders = nil

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for external keycloak without auth providers")
	}
}

func TestValidate_ExternalKeycloak_RequiresIssuerURL(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest.Keycloak.Mode = AuthModeExternal
	cd.Spec.Rest.Keycloak.AuthProviders = []AuthProviderConfig{
		{
			Name:      "test",
			IssuerURL: "",
			JWKSURL:   "https://example.com/.well-known/jwks.json",
		},
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for auth provider without issuerURL")
	}
}

func TestValidate_ExternalKeycloak_RequiresJWKSURL(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest.Keycloak.Mode = AuthModeExternal
	cd.Spec.Rest.Keycloak.AuthProviders = []AuthProviderConfig{
		{
			Name:      "test",
			IssuerURL: "https://example.com",
			JWKSURL:   "",
		},
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for auth provider without jwksURL")
	}
}

func TestValidate_ExternalKeycloak_ValidPasses(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest.Keycloak.Mode = AuthModeExternal
	cd.Spec.Rest.Keycloak.AuthProviders = []AuthProviderConfig{
		{
			Name:      "test",
			IssuerURL: "https://example.com",
			JWKSURL:   "https://example.com/.well-known/jwks.json",
		},
	}

	_, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_DisabledKeycloak_ReturnsWarning(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest.Keycloak.Mode = AuthModeDisabled

	warnings, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	found := false
	for _, w := range warnings {
		if w == "Authentication disabled - not recommended for production" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about disabled authentication")
	}
}

func TestValidate_ExternalTemporal_RequiresEndpoint(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest.Temporal.Mode = ExternalMode
	cd.Spec.Rest.Temporal.Endpoint = ""

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for external temporal without endpoint")
	}
}

func TestValidate_ExternalTemporal_ValidPasses(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest.Temporal.Mode = ExternalMode
	cd.Spec.Rest.Temporal.Endpoint = "temporal.example.com:7233"

	_, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_CertManagerTLS_RequiresIssuerRefName(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.TLS = &TLSConfig{
		Mode: TLSModeCertManager,
		CertManager: &CertManagerConfig{
			IssuerRef: CertManagerIssuerRef{
				Name: "",
				Kind: "ClusterIssuer",
			},
		},
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for cert-manager without issuer name")
	}
}

func TestValidate_CertManagerTLS_RequiresIssuerRefKind(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.TLS = &TLSConfig{
		Mode: TLSModeCertManager,
		CertManager: &CertManagerConfig{
			IssuerRef: CertManagerIssuerRef{
				Name: "my-issuer",
				Kind: "",
			},
		},
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for cert-manager without issuer kind")
	}
}

func TestValidate_CertManagerTLS_NilCertManager(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.TLS = &TLSConfig{
		Mode: TLSModeCertManager,
	}

	_, err := cd.validateCarbideDeployment()
	if err == nil {
		t.Error("expected error for cert-manager mode without certManager config")
	}
}

func TestValidate_CertManagerTLS_ValidPasses(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.TLS = &TLSConfig{
		Mode: TLSModeCertManager,
		CertManager: &CertManagerConfig{
			IssuerRef: CertManagerIssuerRef{
				Name: "my-issuer",
				Kind: "ClusterIssuer",
			},
		},
	}

	_, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_SPIFFEMode_ReturnsWarning(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.TLS = &TLSConfig{
		Mode: TLSModeSpiffe,
	}

	warnings, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	found := false
	for _, w := range warnings {
		if w == "SPIRE must be installed in the cluster for SPIFFE TLS mode" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about SPIRE installation")
	}
}

func TestValidate_RLA_NotSupportedForManagement(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Core.RLA = &RLAConfig{Enabled: true}

	warnings, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	found := false
	for _, w := range warnings {
		if w == "core.rla is only supported for site and management-with-site profiles" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about RLA not supported for management profile")
	}
}

func TestValidate_PSM_NotSupportedForManagement(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Core.PSM = &PSMConfig{Enabled: true}

	warnings, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	found := false
	for _, w := range warnings {
		if w == "core.psm is only supported for site and management-with-site profiles" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about PSM not supported for management profile")
	}
}

func TestValidate_RLA_NoWarningForSiteProfile(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileSite)
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.IP = "10.0.0.1"
	cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"
	cd.Spec.Core.RLA = &RLAConfig{Enabled: true}

	warnings, err := cd.validateCarbideDeployment()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	for _, w := range warnings {
		if w == "core.rla is only supported for site and management-with-site profiles" {
			t.Error("did not expect RLA warning for site profile")
		}
	}
}

func TestValidate_ManagementProfile_NetworkWarning(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Network.Interface = "eth0"
	cd.Spec.Network.AdminNetworkCIDR = "10.0.0.0/24"

	warnings, _ := cd.validateCarbideDeployment()
	found := false
	for _, w := range warnings {
		if w == "network.interface and network.adminNetworkCIDR are not used for management profile" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning about unused network config for management profile")
	}
}

func TestValidateCreate_DelegatesToValidate(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest = nil

	_, err := cd.ValidateCreate(context.Background(), cd)
	if err == nil {
		t.Error("expected ValidateCreate to return error from validateCarbideDeployment")
	}
}

func TestValidateUpdate_DelegatesToValidate(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)
	cd.Spec.Rest = nil

	_, err := cd.ValidateUpdate(context.Background(), nil, cd)
	if err == nil {
		t.Error("expected ValidateUpdate to return error from validateCarbideDeployment")
	}
}

func TestValidateDelete_NoError(t *testing.T) {
	cd := newCarbideDeployment("test", ProfileManagement)

	warnings, err := cd.ValidateDelete(context.Background(), cd)
	if err != nil {
		t.Errorf("expected no error from ValidateDelete, got %v", err)
	}
	if warnings != nil {
		t.Errorf("expected no warnings from ValidateDelete, got %v", warnings)
	}
}

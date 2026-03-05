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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var carbidedeploymentlog = logf.Log.WithName("carbidedeployment-resource")

// SetupWebhookWithManager registers the webhook with the manager
func (r *CarbideDeployment) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-carbide-nvidia-com-v1alpha1-carbidedeployment,mutating=true,failurePolicy=fail,sideEffects=None,groups=carbide.nvidia.com,resources=carbidedeployments,verbs=create;update,versions=v1alpha1,name=mcarbidedeployment.kb.io,admissionReviewVersions=v1

// Default implements defaulting logic for CarbideDeployment
func (r *CarbideDeployment) Default() {
	carbidedeploymentlog.Info("applying defaults", "name", r.Name, "profile", r.Spec.Profile)

	// Set namespace defaults based on profile
	if r.Spec.Infrastructure != nil && r.Spec.Infrastructure.Namespace == "" {
		switch r.Spec.Profile {
		case ProfileManagement:
			r.Spec.Infrastructure.Namespace = "nvidia-carbide-mgmt"
		case ProfileSite:
			r.Spec.Infrastructure.Namespace = fmt.Sprintf("carbide-site-%s", r.Name)
		case ProfileManagementWithSite:
			r.Spec.Infrastructure.Namespace = "nvidia-carbide"
		}
	}

	// Core and Rest namespaces default to Infrastructure namespace
	if r.Spec.Core.Namespace == "" && r.Spec.Infrastructure != nil {
		r.Spec.Core.Namespace = r.Spec.Infrastructure.Namespace
	}
	if r.Spec.Rest != nil && r.Spec.Rest.Namespace == "" && r.Spec.Infrastructure != nil {
		r.Spec.Rest.Namespace = r.Spec.Infrastructure.Namespace
	}

	// Enable/disable components based on profile
	switch r.Spec.Profile {
	case ProfileManagement:
		// Management profile: only REST tier, no site services
		r.Spec.Core.DHCP.Enabled = false
		r.Spec.Core.PXE.Enabled = false
		r.Spec.Core.DNS.Enabled = false
		// carbide-api not needed for management-only
		if r.Spec.Rest != nil {
			r.Spec.Rest.Enabled = true
		}

	case ProfileSite:
		// Site profile: only site services, no REST tier
		r.Spec.Core.DHCP.Enabled = true
		r.Spec.Core.PXE.Enabled = true
		r.Spec.Core.DNS.Enabled = true
		// Disable REST tier for site-only deployments
		if r.Spec.Rest != nil {
			r.Spec.Rest.Enabled = false
		}

	case ProfileManagementWithSite:
		// Management-with-site: enable all services
		r.Spec.Core.DHCP.Enabled = true
		r.Spec.Core.PXE.Enabled = true
		r.Spec.Core.DNS.Enabled = true
		if r.Spec.Rest != nil {
			r.Spec.Rest.Enabled = true
		}
	}

	// Set database list based on profile if not specified
	if r.Spec.Infrastructure != nil && len(r.Spec.Infrastructure.PostgreSQL.Databases) == 0 {
		switch r.Spec.Profile {
		case ProfileManagement:
			r.Spec.Infrastructure.PostgreSQL.Databases = []string{
				"forge", "temporal", "temporal_visibility", "keycloak",
			}
		case ProfileSite:
			r.Spec.Infrastructure.PostgreSQL.Databases = []string{
				"carbide", "forge", "rla", "psm",
			}
		case ProfileManagementWithSite:
			r.Spec.Infrastructure.PostgreSQL.Databases = []string{
				"carbide", "forge", "rla", "psm", "temporal", "temporal_visibility", "keycloak",
			}
		}
	}

	// Set default domain if not specified
	if r.Spec.Network.Domain == "" {
		r.Spec.Network.Domain = "carbide.local"
	}

	// Set default API port if not specified
	if r.Spec.Core.API.Port == 0 {
		r.Spec.Core.API.Port = 1079
	}

	// Set default API replicas if not specified
	if r.Spec.Core.API.Replicas == 0 {
		r.Spec.Core.API.Replicas = 1
	}

	// Set default PXE ports if not specified
	if r.Spec.Core.PXE.TFTPPort == 0 {
		r.Spec.Core.PXE.TFTPPort = 69
	}
	if r.Spec.Core.PXE.HTTPPort == 0 {
		r.Spec.Core.PXE.HTTPPort = 8080
	}

	// Set default DNS port if not specified
	if r.Spec.Core.DNS.Port == 0 {
		r.Spec.Core.DNS.Port = 53
	}

	// Set default REST API port if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.RestAPI.Port == 0 {
		r.Spec.Rest.RestAPI.Port = 8080
	}

	// Set default REST API replicas if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.RestAPI.Replicas == 0 {
		r.Spec.Rest.RestAPI.Replicas = 1
	}

	// Set default PostgreSQL mode if not specified
	if r.Spec.Infrastructure != nil && r.Spec.Infrastructure.PostgreSQL.Mode == "" {
		r.Spec.Infrastructure.PostgreSQL.Mode = ManagedMode
	}

	// Set default Temporal mode if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.Temporal.Mode == "" {
		r.Spec.Rest.Temporal.Mode = ManagedMode
	}

	// Set default Keycloak mode if not specified
	if r.Spec.Rest != nil && string(r.Spec.Rest.Keycloak.Mode) == "" {
		r.Spec.Rest.Keycloak.Mode = AuthModeManaged
	}

	// Set default Keycloak realm if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.Keycloak.Realm == "" {
		r.Spec.Rest.Keycloak.Realm = "carbide"
	}

	// Set default Temporal version if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.Temporal.Version == "" {
		r.Spec.Rest.Temporal.Version = "1.22.0"
	}

	// Set default Temporal chart version if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.Temporal.ChartVersion == "" {
		r.Spec.Rest.Temporal.ChartVersion = "0.73.1"
	}

	// Set default Temporal namespace if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.Temporal.Namespace == "" {
		r.Spec.Rest.Temporal.Namespace = "temporal"
	}

	// Set default PostgreSQL replicas if not specified
	if r.Spec.Infrastructure != nil && r.Spec.Infrastructure.PostgreSQL.Replicas == 0 {
		r.Spec.Infrastructure.PostgreSQL.Replicas = 1
	}

	// Set default Temporal replicas if not specified
	if r.Spec.Rest != nil && r.Spec.Rest.Temporal.Replicas == 0 {
		r.Spec.Rest.Temporal.Replicas = 1
	}

	// Set TLS defaults
	if r.Spec.TLS == nil {
		r.Spec.TLS = &TLSConfig{
			Mode: TLSModeSpiffe,
			SPIFFE: &SPIFFEConfig{
				TrustDomain: "carbide.local",
				HelperImage: "ghcr.io/nvidia/spiffe-helper:latest",
				ClassName:   "zero-trust-workload-identity-manager-spire",
			},
		}
	} else {
		if r.Spec.TLS.Mode == "" {
			r.Spec.TLS.Mode = TLSModeSpiffe
		}
		if r.Spec.TLS.Mode == TLSModeSpiffe && r.Spec.TLS.SPIFFE == nil {
			r.Spec.TLS.SPIFFE = &SPIFFEConfig{}
		}
		if r.Spec.TLS.SPIFFE != nil {
			if r.Spec.TLS.SPIFFE.TrustDomain == "" {
				r.Spec.TLS.SPIFFE.TrustDomain = "carbide.local"
			}
			if r.Spec.TLS.SPIFFE.HelperImage == "" {
				r.Spec.TLS.SPIFFE.HelperImage = "ghcr.io/nvidia/spiffe-helper:latest"
			}
			if r.Spec.TLS.SPIFFE.ClassName == "" {
				r.Spec.TLS.SPIFFE.ClassName = "zero-trust-workload-identity-manager-spire"
			}
		}
	}

	// Set Vault defaults for site profiles
	isSiteProfile := r.Spec.Profile == ProfileSite || r.Spec.Profile == ProfileManagementWithSite
	if isSiteProfile && r.Spec.Core.Vault == nil {
		r.Spec.Core.Vault = &VaultConfig{
			Mode:        ManagedMode,
			Version:     "1.15.6",
			KVMountPath: "secrets",
		}
	}
	if r.Spec.Core.Vault != nil {
		if r.Spec.Core.Vault.Mode == "" {
			r.Spec.Core.Vault.Mode = ManagedMode
		}
		if r.Spec.Core.Vault.Version == "" {
			r.Spec.Core.Vault.Version = "1.15.6"
		}
		if r.Spec.Core.Vault.KVMountPath == "" {
			r.Spec.Core.Vault.KVMountPath = "secrets"
		}
	}

	// Set RLA defaults for site profiles
	if isSiteProfile && r.Spec.Core.RLA == nil {
		r.Spec.Core.RLA = &RLAConfig{
			Enabled:  true,
			Port:     50051,
			Replicas: 1,
		}
	}
	if r.Spec.Core.RLA != nil {
		if r.Spec.Core.RLA.Port == 0 {
			r.Spec.Core.RLA.Port = 50051
		}
		if r.Spec.Core.RLA.Replicas == 0 {
			r.Spec.Core.RLA.Replicas = 1
		}
	}

	// Set PSM defaults for site profiles
	if isSiteProfile && r.Spec.Core.PSM == nil {
		r.Spec.Core.PSM = &PSMConfig{
			Enabled:  true,
			Port:     50051,
			Replicas: 1,
		}
	}
	if r.Spec.Core.PSM != nil {
		if r.Spec.Core.PSM.Port == 0 {
			r.Spec.Core.PSM.Port = 50051
		}
		if r.Spec.Core.PSM.Replicas == 0 {
			r.Spec.Core.PSM.Replicas = 1
		}
	}
}

// +kubebuilder:webhook:path=/validate-carbide-nvidia-com-v1alpha1-carbidedeployment,mutating=false,failurePolicy=fail,sideEffects=None,groups=carbide.nvidia.com,resources=carbidedeployments,verbs=create;update,versions=v1alpha1,name=vcarbidedeployment.kb.io,admissionReviewVersions=v1

// ValidateCreate implements validation logic for create operations
func (r *CarbideDeployment) ValidateCreate() (admission.Warnings, error) {
	carbidedeploymentlog.Info("validating create", "name", r.Name)

	return r.validateCarbideDeployment()
}

// ValidateUpdate implements webhook.Validator
func (r *CarbideDeployment) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	carbidedeploymentlog.Info("validating update", "name", r.Name)

	return r.validateCarbideDeployment()
}

// ValidateDelete implements webhook.Validator
func (r *CarbideDeployment) ValidateDelete() (admission.Warnings, error) {
	carbidedeploymentlog.Info("validating delete", "name", r.Name)

	// No validation needed for delete
	return nil, nil
}

// validateCarbideDeployment performs common validation
func (r *CarbideDeployment) validateCarbideDeployment() (admission.Warnings, error) {
	var warnings admission.Warnings

	// Validate network config for site profiles
	if r.Spec.Profile == ProfileSite || r.Spec.Profile == ProfileManagementWithSite {
		if r.Spec.Network.Interface == "" {
			return warnings, fmt.Errorf("network.interface is required for %s profile", r.Spec.Profile)
		}
		if r.Spec.Network.AdminNetworkCIDR == "" {
			return warnings, fmt.Errorf("network.adminNetworkCIDR is required for %s profile", r.Spec.Profile)
		}
		if r.Spec.Network.IP == "" {
			return warnings, fmt.Errorf("network.ip is required for %s profile", r.Spec.Profile)
		}
	}

	// Validate management profile doesn't have network config
	if r.Spec.Profile == ProfileManagement {
		if r.Spec.Network.Interface != "" || r.Spec.Network.AdminNetworkCIDR != "" {
			warnings = append(warnings, "network.interface and network.adminNetworkCIDR are not used for management profile")
		}
	}

	// Validate REST tier is enabled for management profiles
	if r.Spec.Profile == ProfileManagement || r.Spec.Profile == ProfileManagementWithSite {
		if r.Spec.Rest == nil {
			return warnings, fmt.Errorf("rest tier configuration is required for %s profile", r.Spec.Profile)
		}
	}

	// Validate TLS configuration
	if r.Spec.TLS != nil {
		switch r.Spec.TLS.Mode {
		case TLSModeCertManager:
			if r.Spec.TLS.CertManager == nil || r.Spec.TLS.CertManager.IssuerRef.Name == "" {
				return warnings, fmt.Errorf("tls.certManager.issuerRef is required when tls.mode is certManager")
			}
			if r.Spec.TLS.CertManager.IssuerRef.Kind == "" {
				return warnings, fmt.Errorf("tls.certManager.issuerRef.kind is required (Issuer or ClusterIssuer)")
			}
		case TLSModeSpiffe:
			warnings = append(warnings, "SPIRE must be installed in the cluster for SPIFFE TLS mode")
		}
	}

	// Validate external PostgreSQL configuration
	if r.Spec.Infrastructure != nil && r.Spec.Infrastructure.PostgreSQL.Mode == ExternalMode {
		if r.Spec.Infrastructure.PostgreSQL.Connection == nil {
			return warnings, fmt.Errorf("infrastructure.postgresql.connection is required when mode is external")
		}
		if r.Spec.Infrastructure.PostgreSQL.Connection.Host == "" {
			return warnings, fmt.Errorf("infrastructure.postgresql.connection.host is required")
		}
		// External mode: require userSecrets for each database
		if len(r.Spec.Infrastructure.PostgreSQL.Connection.UserSecrets) == 0 {
			warnings = append(warnings, "infrastructure.postgresql.connection.userSecrets is recommended for external mode")
		}
	}

	// Validate Vault configuration
	if r.Spec.Core.Vault != nil && r.Spec.Core.Vault.Mode == ExternalMode {
		if r.Spec.Core.Vault.Address == "" {
			return warnings, fmt.Errorf("core.vault.address is required when vault mode is external")
		}
		if r.Spec.Core.Vault.TokenSecretRef == nil {
			return warnings, fmt.Errorf("core.vault.tokenSecretRef is required when vault mode is external")
		}
	}

	// Validate RLA/PSM only for site profiles
	isSiteProfile := r.Spec.Profile == ProfileSite || r.Spec.Profile == ProfileManagementWithSite
	if !isSiteProfile {
		if r.Spec.Core.RLA != nil && r.Spec.Core.RLA.Enabled {
			warnings = append(warnings, "core.rla is only supported for site and management-with-site profiles")
		}
		if r.Spec.Core.PSM != nil && r.Spec.Core.PSM.Enabled {
			warnings = append(warnings, "core.psm is only supported for site and management-with-site profiles")
		}
	}

	// Validate external Temporal configuration
	if r.Spec.Rest != nil && r.Spec.Rest.Temporal.Mode == ExternalMode {
		if r.Spec.Rest.Temporal.Endpoint == "" {
			return warnings, fmt.Errorf("rest.temporal.endpoint is required when mode is external")
		}
	}

	// Validate Keycloak configuration
	if r.Spec.Rest != nil {
		switch r.Spec.Rest.Keycloak.Mode {
		case AuthModeExternal:
			if len(r.Spec.Rest.Keycloak.AuthProviders) == 0 {
				return warnings, fmt.Errorf("rest.keycloak.authProviders is required when mode is external (at least 1 provider)")
			}
			for i, provider := range r.Spec.Rest.Keycloak.AuthProviders {
				if provider.IssuerURL == "" {
					return warnings, fmt.Errorf("rest.keycloak.authProviders[%d].issuerURL is required", i)
				}
				if provider.JWKSURL == "" {
					return warnings, fmt.Errorf("rest.keycloak.authProviders[%d].jwksURL is required", i)
				}
			}
		case AuthModeDisabled:
			warnings = append(warnings, "Authentication disabled - not recommended for production")
		}
	}

	return warnings, nil
}

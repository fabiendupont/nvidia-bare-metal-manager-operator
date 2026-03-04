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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeploymentMode defines whether a component is managed by the operator or externally provisioned
// +kubebuilder:validation:Enum=managed;external
type DeploymentMode string

const (
	// ManagedMode indicates the operator manages the component lifecycle
	ManagedMode DeploymentMode = "managed"
	// ExternalMode indicates the component is externally provisioned
	ExternalMode DeploymentMode = "external"
)

// DeploymentProfile defines the type of BMM deployment
// +kubebuilder:validation:Enum=management;site;management-with-site
type DeploymentProfile string

const (
	// ProfileManagement deploys only management components (Temporal, Keycloak, REST API)
	ProfileManagement DeploymentProfile = "management"
	// ProfileSite deploys only site components (carbide-api, DHCP, PXE, DNS)
	ProfileSite DeploymentProfile = "site"
	// ProfileManagementWithSite deploys all components (management + site)
	ProfileManagementWithSite DeploymentProfile = "management-with-site"
)

// DeploymentPhase represents the overall deployment state
// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Failed;Updating
type DeploymentPhase string

const (
	PhasePending      DeploymentPhase = "Pending"
	PhaseProvisioning DeploymentPhase = "Provisioning"
	PhaseReady        DeploymentPhase = "Ready"
	PhaseFailed       DeploymentPhase = "Failed"
	PhaseUpdating     DeploymentPhase = "Updating"
)

// TLSMode selects the TLS backend for mTLS
// +kubebuilder:validation:Enum=spiffe;certManager
type TLSMode string

const (
	TLSModeSpiffe      TLSMode = "spiffe"
	TLSModeCertManager TLSMode = "certManager"
)

// AuthMode defines authentication deployment mode (managed, external, or disabled)
// +kubebuilder:validation:Enum=managed;external;disabled
type AuthMode string

const (
	AuthModeManaged  AuthMode = "managed"
	AuthModeExternal AuthMode = "external"
	AuthModeDisabled AuthMode = "disabled"
)

// CarbideDeploymentSpec defines the desired state of CarbideDeployment
type CarbideDeploymentSpec struct {
	// Profile determines which components are deployed (management, site, or management-with-site)
	// +kubebuilder:validation:Required
	Profile DeploymentProfile `json:"profile"`

	// Version is the BMM version to deploy
	// +kubebuilder:validation:Required
	// +kubebuilder:default:="latest"
	Version string `json:"version"`

	// Network configuration for BMM services
	// +kubebuilder:validation:Required
	Network NetworkConfig `json:"network"`

	// Infrastructure tier configuration (PostgreSQL)
	// +optional
	Infrastructure *InfrastructureConfig `json:"infrastructure,omitempty"`

	// Core tier configuration (carbide-api, DHCP, PXE, DNS, Vault, RLA, PSM)
	// +kubebuilder:validation:Required
	Core CoreConfig `json:"core"`

	// Rest tier configuration (Temporal, Keycloak, REST API, Site Agent)
	// +optional
	Rest *RestConfig `json:"rest,omitempty"`

	// Images configuration for overriding default images
	// +optional
	Images *ImageConfig `json:"images,omitempty"`

	// TLS configuration for mTLS (replaces SPIFFE-only config)
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`

	// SiteRef links this deployment to a Site CRD in the hub cluster
	// Documents the hub-spoke relationship for site profile deployments
	// +optional
	SiteRef *SiteRef `json:"siteRef,omitempty"`
}

// TLSConfig defines TLS backend configuration for mTLS
type TLSConfig struct {
	// Mode selects the TLS backend
	// +kubebuilder:default:=spiffe
	// +optional
	Mode TLSMode `json:"mode,omitempty"`

	// SPIFFE settings (used when mode=spiffe)
	// +optional
	SPIFFE *SPIFFEConfig `json:"spiffe,omitempty"`

	// CertManager settings (used when mode=certManager)
	// +optional
	CertManager *CertManagerConfig `json:"certManager,omitempty"`
}

// SPIFFEConfig defines SPIFFE/SPIRE mTLS settings
type SPIFFEConfig struct {
	// TrustDomain is the SPIFFE trust domain
	// +kubebuilder:default:="carbide.local"
	// +optional
	TrustDomain string `json:"trustDomain,omitempty"`

	// HelperImage is the spiffe-helper container image
	// +kubebuilder:default:="ghcr.io/nvidia/spiffe-helper:latest"
	// +optional
	HelperImage string `json:"helperImage,omitempty"`

	// ClassName is the SPIRE class name for ClusterSPIFFEID
	// +kubebuilder:default:="zero-trust-workload-identity-manager-spire"
	// +optional
	ClassName string `json:"className,omitempty"`
}

// CertManagerConfig defines cert-manager TLS settings
type CertManagerConfig struct {
	// IssuerRef references the cert-manager Issuer/ClusterIssuer
	IssuerRef CertManagerIssuerRef `json:"issuerRef"`
}

// CertManagerIssuerRef references a cert-manager issuer
type CertManagerIssuerRef struct {
	// Name of the issuer
	Name string `json:"name"`
	// Kind is Issuer or ClusterIssuer
	Kind string `json:"kind"`
	// Group defaults to cert-manager.io
	// +optional
	Group string `json:"group,omitempty"`
}

// NetworkConfig defines network settings for BMM deployment
type NetworkConfig struct {
	// Interface is the network interface name for provisioning network (required for site profiles only)
	// +optional
	Interface string `json:"interface,omitempty"`

	// IP is the IP address for provisioning network (required for site profiles only)
	// +kubebuilder:validation:Pattern=`^(\d{1,3}\.){3}\d{1,3}$`
	// +optional
	IP string `json:"ip,omitempty"`

	// AdminNetworkCIDR is the provisioning network CIDR (required for site profiles only)
	// +kubebuilder:validation:Pattern=`^(\d{1,3}\.){3}\d{1,3}/\d{1,2}$`
	// +optional
	AdminNetworkCIDR string `json:"adminNetworkCIDR,omitempty"`

	// Domain is the DNS domain for all services
	// +kubebuilder:default:="carbide.local"
	// +optional
	Domain string `json:"domain,omitempty"`
}

// InfrastructureConfig defines infrastructure tier components
type InfrastructureConfig struct {
	// StorageClass for PVCs (uses cluster default if not specified)
	// +optional
	StorageClass string `json:"storageClass,omitempty"`

	// PostgreSQL database configuration
	// +kubebuilder:validation:Required
	PostgreSQL PostgreSQLConfig `json:"postgresql"`

	// Namespace for infrastructure components
	// Defaults based on profile:
	//   - management: "carbide-mgmt"
	//   - site: "carbide-site-{deployment-name}"
	//   - management-with-site: "carbide"
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// PostgreSQLConfig defines PostgreSQL deployment configuration
type PostgreSQLConfig struct {
	// Mode determines if PostgreSQL is managed or external
	// +kubebuilder:default:=managed
	// +optional
	Mode DeploymentMode `json:"mode,omitempty"`

	// Databases to create (auto-configured based on profile if not specified)
	// For management: temporal, temporal_visibility, keycloak, forge
	// For site: carbide, forge, rla, psm
	// For management-with-site: all databases
	// +optional
	Databases []string `json:"databases,omitempty"`

	// --- Managed mode fields ---

	// Version is the PostgreSQL version (for managed mode)
	// +kubebuilder:validation:Pattern=`^\d+$`
	// +optional
	Version string `json:"version,omitempty"`

	// Storage configuration for managed PostgreSQL
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// Replicas is the number of PostgreSQL replicas (for managed mode)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for PostgreSQL pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// --- External mode fields ---

	// Connection details for external PostgreSQL
	// +optional
	Connection *ExternalPGConnection `json:"connection,omitempty"`
}

// ExternalPGConnection defines connection to external PostgreSQL
type ExternalPGConnection struct {
	// Host is the PostgreSQL server hostname
	// +kubebuilder:validation:Required
	Host string `json:"host"`

	// Port is the PostgreSQL server port
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=5432
	// +optional
	Port int32 `json:"port,omitempty"`

	// SSLMode for PostgreSQL connection
	// +kubebuilder:validation:Enum=disable;require;verify-ca;verify-full
	// +kubebuilder:default:="require"
	// +optional
	SSLMode string `json:"sslMode,omitempty"`

	// UserSecrets maps database user names to secrets containing credentials.
	// Each secret must contain keys: "username", "password", "dbname"
	// +optional
	UserSecrets map[string]SecretRef `json:"userSecrets,omitempty"`
}

// CoreConfig defines core tier components
type CoreConfig struct {
	// Namespace for core components
	// Defaults to same as Infrastructure.Namespace if not specified
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// API is the carbide-api configuration
	// +kubebuilder:validation:Required
	API APIConfig `json:"api"`

	// DHCP server configuration
	// +kubebuilder:validation:Required
	DHCP DHCPConfig `json:"dhcp"`

	// PXE boot server configuration
	// +kubebuilder:validation:Required
	PXE PXEConfig `json:"pxe"`

	// DNS server configuration
	// +kubebuilder:validation:Required
	DNS DNSConfig `json:"dns"`

	// Security configuration
	// +optional
	Security *SecurityConfig `json:"security,omitempty"`

	// Vault for BMC credential storage (site profiles only)
	// +optional
	Vault *VaultConfig `json:"vault,omitempty"`

	// RLA (Rack Level Administration) configuration
	// +optional
	RLA *RLAConfig `json:"rla,omitempty"`

	// PSM (Power Shelf Manager) configuration
	// +optional
	PSM *PSMConfig `json:"psm,omitempty"`
}

// APIConfig defines carbide-api settings
type APIConfig struct {
	// Port for the gRPC API
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=1079
	// +optional
	Port int32 `json:"port,omitempty"`

	// Replicas for carbide-api
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for carbide-api pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// DHCPConfig defines DHCP server settings
type DHCPConfig struct {
	// Enabled controls DHCP server deployment
	// +kubebuilder:default:=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Resources for DHCP pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// PXEConfig defines PXE boot server settings
type PXEConfig struct {
	// Enabled controls PXE server deployment
	// +kubebuilder:default:=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Storage for PXE boot files
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// Resources for PXE pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// TFTPPort for TFTP service
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=69
	// +optional
	TFTPPort int32 `json:"tftpPort,omitempty"`

	// HTTPPort for iPXE script server
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=8080
	// +optional
	HTTPPort int32 `json:"httpPort,omitempty"`
}

// DNSConfig defines DNS server settings
type DNSConfig struct {
	// Enabled controls DNS server deployment
	// +kubebuilder:default:=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Resources for DNS pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Port for DNS service
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=53
	// +optional
	Port int32 `json:"port,omitempty"`
}

// SecurityConfig defines security settings
type SecurityConfig struct {
	// TLSEnabled controls TLS for API communications
	// +kubebuilder:default:=true
	// +optional
	TLSEnabled bool `json:"tlsEnabled,omitempty"`

	// RBACBypass disables RBAC (UNSAFE for production)
	// +optional
	RBACBypass bool `json:"rbacBypass,omitempty"`
}

// VaultConfig defines Vault deployment configuration
type VaultConfig struct {
	// Mode determines if Vault is managed or external
	// +kubebuilder:default:=managed
	// +optional
	Mode DeploymentMode `json:"mode,omitempty"`

	// --- Managed mode ---

	// Version of Vault Helm chart to deploy
	// +kubebuilder:default:="1.15.6"
	// +optional
	Version string `json:"version,omitempty"`

	// Storage configuration for managed Vault
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// --- External mode ---

	// Address of external Vault instance
	// +optional
	Address string `json:"address,omitempty"`

	// TokenSecretRef references a secret containing "token" key
	// +optional
	TokenSecretRef *SecretRef `json:"tokenSecretRef,omitempty"`

	// KVMountPath for the KV v2 secrets engine
	// +kubebuilder:default:="secrets"
	// +optional
	KVMountPath string `json:"kvMountPath,omitempty"`
}

// RLAConfig defines RLA (Rack Level Administration) configuration
type RLAConfig struct {
	// Enabled controls RLA deployment
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Port for gRPC service
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=50051
	// +optional
	Port int32 `json:"port,omitempty"`

	// Replicas for RLA
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for RLA pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// PSMConfig defines PSM (Power Shelf Manager) configuration
type PSMConfig struct {
	// Enabled controls PSM deployment
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Port for gRPC service
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=50051
	// +optional
	Port int32 `json:"port,omitempty"`

	// Replicas for PSM
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for PSM pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// RestConfig defines REST tier components
type RestConfig struct {
	// Namespace for REST components
	// Defaults to same as Infrastructure.Namespace if not specified
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Enabled controls REST tier deployment
	// +kubebuilder:default:=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Temporal workflow engine configuration
	// +kubebuilder:validation:Required
	Temporal TemporalConfig `json:"temporal"`

	// Keycloak authentication configuration
	// +kubebuilder:validation:Required
	Keycloak KeycloakConfig `json:"keycloak"`

	// RestAPI configuration
	// +kubebuilder:validation:Required
	RestAPI RestAPIConfig `json:"restAPI"`

	// SiteAgent configuration (for spoke deployments)
	// +optional
	SiteAgent *SiteAgentConfig `json:"siteAgent,omitempty"`
}

// TemporalConfig defines Temporal deployment configuration
type TemporalConfig struct {
	// Mode determines if Temporal is managed or external
	// +kubebuilder:default:=managed
	// +optional
	Mode DeploymentMode `json:"mode,omitempty"`

	// --- Managed mode fields ---

	// Version is the Temporal server version (for managed mode)
	// +kubebuilder:default:="1.22.0"
	// +optional
	Version string `json:"version,omitempty"`

	// ChartVersion is the Temporal Helm chart version
	// +kubebuilder:default:="0.73.1"
	// +optional
	ChartVersion string `json:"chartVersion,omitempty"`

	// Namespace for Temporal resources
	// +kubebuilder:default:="temporal"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Replicas is the number of Temporal frontend instances (for managed mode)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for Temporal pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// --- External mode fields ---

	// Endpoint is the external Temporal frontend URL
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// TLSSecretRef for client certs when connecting to Temporal
	// +optional
	TLSSecretRef *SecretRef `json:"tlsSecretRef,omitempty"`
}

// KeycloakConfig defines Keycloak deployment configuration
type KeycloakConfig struct {
	// Mode determines if Keycloak is managed, external, or disabled
	// +kubebuilder:default:=managed
	// +optional
	Mode AuthMode `json:"mode,omitempty"`

	// Realm name (required for managed and external modes)
	// +kubebuilder:default:="carbide"
	// +optional
	Realm string `json:"realm,omitempty"`

	// --- Managed mode fields ---

	// AdminPasswordSecretRef for Keycloak admin user
	// +optional
	AdminPasswordSecretRef *SecretRef `json:"adminPasswordSecretRef,omitempty"`

	// Resources for Keycloak pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// --- External mode fields (any OIDC provider) ---

	// AuthProviders list (at least 1 required when mode=external)
	// +optional
	AuthProviders []AuthProviderConfig `json:"authProviders,omitempty"`
}

// AuthProviderConfig defines an external OIDC auth provider
type AuthProviderConfig struct {
	// Name identifies the auth provider
	Name string `json:"name"`

	// IssuerURL is the OIDC issuer URL
	IssuerURL string `json:"issuerURL"`

	// JWKSURL is the JWKS endpoint URL
	JWKSURL string `json:"jwksURL"`

	// ClientID for the OIDC client
	ClientID string `json:"clientID"`

	// ClientSecretRef references a secret containing client credentials
	// +optional
	ClientSecretRef *SecretRef `json:"clientSecretRef,omitempty"`
}

// RestAPIConfig defines REST API settings
type RestAPIConfig struct {
	// Port for the REST API
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default:=8080
	// +optional
	Port int32 `json:"port,omitempty"`

	// NodePort for external access
	// +kubebuilder:validation:Minimum=30000
	// +kubebuilder:validation:Maximum=32767
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`

	// Replicas for REST API
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default:=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Resources for REST API pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// SiteAgentConfig defines site agent settings (for spoke deployments)
type SiteAgentConfig struct {
	// Enabled controls site agent deployment
	// +kubebuilder:default:=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// HubTemporalEndpoint for spoke-to-hub connection
	// +optional
	HubTemporalEndpoint string `json:"hubTemporalEndpoint,omitempty"`

	// Resources for site agent pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ImageConfig defines container image overrides
type ImageConfig struct {
	// Registry is the container registry
	// +kubebuilder:default:="ghcr.io/nvidia"
	// +optional
	Registry string `json:"registry,omitempty"`

	// BMMCore image
	// +optional
	BMMCore string `json:"bmmCore,omitempty"`

	// RestAPI image
	// +optional
	RestAPI string `json:"restAPI,omitempty"`

	// SiteAgent image
	// +optional
	SiteAgent string `json:"siteAgent,omitempty"`

	// SiteManager image
	// +optional
	SiteManager string `json:"siteManager,omitempty"`

	// Workflow image (for cloud-worker and site-worker)
	// +optional
	Workflow string `json:"workflow,omitempty"`

	// RestDB image (for REST API database migrations)
	// +optional
	RestDB string `json:"restDB,omitempty"`

	// RLA image
	// +optional
	RLA string `json:"rla,omitempty"`

	// PSM image
	// +optional
	PSM string `json:"psm,omitempty"`

	// DHCP image override
	// +optional
	DHCP string `json:"dhcp,omitempty"`

	// DNS image override
	// +optional
	DNS string `json:"dns,omitempty"`

	// PXE image override
	// +optional
	PXE string `json:"pxe,omitempty"`

	// PullPolicy for images
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default:="IfNotPresent"
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`

	// PullSecrets for private registries
	// +optional
	PullSecrets []corev1.LocalObjectReference `json:"pullSecrets,omitempty"`
}

// StorageSpec defines storage requirements
type StorageSpec struct {
	// Size is the storage size
	// +kubebuilder:validation:Required
	Size resource.Quantity `json:"size"`

	// StorageClass override (uses InfrastructureConfig.StorageClass if not set)
	// +optional
	StorageClass string `json:"storageClass,omitempty"`

	// AccessMode for PVC
	// +kubebuilder:validation:Enum=ReadWriteOnce;ReadOnlyMany;ReadWriteMany
	// +kubebuilder:default:="ReadWriteOnce"
	// +optional
	AccessMode corev1.PersistentVolumeAccessMode `json:"accessMode,omitempty"`
}

// SecretRef references a secret key
type SecretRef struct {
	// Name of the secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key in the secret (defaults to standard keys if not specified)
	// +optional
	Key string `json:"key,omitempty"`

	// UsernameKey for secrets with username/password
	// +optional
	UsernameKey string `json:"usernameKey,omitempty"`

	// PasswordKey for secrets with username/password
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`
}

// SiteRef references a Site CRD in the hub cluster
// This documents the hub-spoke relationship for site deployments
type SiteRef struct {
	// Cluster identifier for the hub cluster
	// Can be a cluster name, URL, or other identifier
	// Omit if Site CRD is in the same cluster
	// +optional
	Cluster string `json:"cluster,omitempty"`

	// Namespace where the Site CRD exists in the hub cluster
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`

	// Name of the Site CRD in the hub cluster
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// UUID of the site for unique identification across clusters
	// Should match the Site CRD's spec.uuid field
	// +optional
	UUID string `json:"uuid,omitempty"`
}

// CarbideDeploymentStatus defines the observed state of CarbideDeployment
type CarbideDeploymentStatus struct {
	// Phase is the overall deployment phase
	// +optional
	Phase DeploymentPhase `json:"phase,omitempty"`

	// Infrastructure tier status
	// +optional
	Infrastructure *TierStatus `json:"infrastructure,omitempty"`

	// Core tier status
	// +optional
	Core *TierStatus `json:"core,omitempty"`

	// Rest tier status
	// +optional
	Rest *TierStatus `json:"rest,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the deployment
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TierStatus represents the status of a deployment tier
type TierStatus struct {
	// Ready indicates if the tier is ready
	Ready bool `json:"ready"`

	// Message provides additional context
	// +optional
	Message string `json:"message,omitempty"`

	// Components status for individual components
	// +optional
	Components []ComponentStatus `json:"components,omitempty"`

	// LastTransitionTime when the tier status changed
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// ComponentStatus represents the status of a single component
type ComponentStatus struct {
	// Name of the component
	Name string `json:"name"`

	// Ready indicates if the component is ready
	Ready bool `json:"ready"`

	// Message provides additional context
	// +optional
	Message string `json:"message,omitempty"`

	// LastTransitionTime when the component status changed
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// Standard condition types for CarbideDeployment
const (
	// ConditionTypeReady indicates the overall ready status
	ConditionTypeReady = "Ready"
	// ConditionTypeInfrastructureReady indicates infrastructure tier is ready
	ConditionTypeInfrastructureReady = "InfrastructureReady"
	// ConditionTypeCoreReady indicates core tier is ready
	ConditionTypeCoreReady = "CoreReady"
	// ConditionTypeRestReady indicates REST tier is ready
	ConditionTypeRestReady = "RestReady"
	// ConditionTypePostgreSQLReady indicates PostgreSQL is ready
	ConditionTypePostgreSQLReady = "PostgreSQLReady"
	// ConditionTypeAPIReady indicates carbide-api is ready
	ConditionTypeAPIReady = "APIReady"
	// ConditionTypeMigrationComplete indicates database migration is complete
	ConditionTypeMigrationComplete = "MigrationComplete"
	// ConditionTypeDHCPReady indicates DHCP server is ready
	ConditionTypeDHCPReady = "DHCPReady"
	// ConditionTypePXEReady indicates PXE server is ready
	ConditionTypePXEReady = "PXEReady"
	// ConditionTypeDNSReady indicates DNS server is ready
	ConditionTypeDNSReady = "DNSReady"
	// ConditionTypeTemporalReady indicates Temporal is ready
	ConditionTypeTemporalReady = "TemporalReady"
	// ConditionTypeKeycloakReady indicates Keycloak is ready
	ConditionTypeKeycloakReady = "KeycloakReady"
	// ConditionTypeRestAPIReady indicates REST API is ready
	ConditionTypeRestAPIReady = "RestAPIReady"
	// ConditionTypeSiteAgentReady indicates site agent is ready
	ConditionTypeSiteAgentReady = "SiteAgentReady"
	// ConditionTypeTLSReady indicates the TLS backend is available
	ConditionTypeTLSReady = "TLSReady"
	// ConditionTypeSPIFFEAvailable indicates SPIRE is detected
	ConditionTypeSPIFFEAvailable = "SPIFFEAvailable"
	// ConditionTypeCertManagerAvailable indicates cert-manager is detected
	ConditionTypeCertManagerAvailable = "CertManagerAvailable"
	// ConditionTypeVaultReady indicates Vault is ready
	ConditionTypeVaultReady = "VaultReady"
	// ConditionTypeRLAReady indicates RLA is ready
	ConditionTypeRLAReady = "RLAReady"
	// ConditionTypePSMReady indicates PSM is ready
	ConditionTypePSMReady = "PSMReady"
	// ConditionTypeSiteManagerReady indicates site-manager is ready
	ConditionTypeSiteManagerReady = "SiteManagerReady"
	// ConditionTypeCloudWorkerReady indicates cloud-worker is ready
	ConditionTypeCloudWorkerReady = "CloudWorkerReady"
	// ConditionTypeSiteWorkerReady indicates site-worker is ready
	ConditionTypeSiteWorkerReady = "SiteWorkerReady"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cd;carbide
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Infrastructure",type=string,JSONPath=`.status.infrastructure.ready`
// +kubebuilder:printcolumn:name="Core",type=string,JSONPath=`.status.core.ready`
// +kubebuilder:printcolumn:name="Rest",type=string,JSONPath=`.status.rest.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +operator-sdk:csv:customresourcedefinitions:displayName="Carbide Deployment",resources={{Deployment,apps/v1},{Service,v1},{ConfigMap,v1},{Secret,v1},{PersistentVolumeClaim,v1},{Job,batch/v1}}

// CarbideDeployment is the Schema for the carbidedeployments API
type CarbideDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CarbideDeploymentSpec   `json:"spec,omitempty"`
	Status CarbideDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CarbideDeploymentList contains a list of CarbideDeployment
type CarbideDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CarbideDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CarbideDeployment{}, &CarbideDeploymentList{})
}

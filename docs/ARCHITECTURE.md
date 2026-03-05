# Carbide Operator Architecture

## Overview

The **carbide-operator** is a Kubernetes operator that deploys and manages BMM's bare metal provisioning infrastructure. It follows a **hub-and-spoke architecture** where management clusters orchestrate provisioning across multiple site clusters.

## Design Principles

1. **Infrastructure-only** - Operator deploys infrastructure, not business entities
2. **Separation of concerns** - Org/provider/site managed via Carbide UI/REST API
3. **GitOps-friendly** - Declarative, reconciliation-based
4. **Profile-based** - Three deployment profiles for different scenarios
5. **Hub-and-spoke** - Management clusters + site clusters

---

## TLS Configuration

The operator supports two TLS backends for mTLS, configured via the top-level `tls:` field:

- **SPIFFE/SPIRE** (`mode: spiffe`, default) - Uses SPIRE for workload identity. SPIRE must be pre-installed on the cluster. The operator creates ClusterSPIFFEID resources for each service.
- **cert-manager** (`mode: certManager`) - Uses cert-manager to issue TLS certificates. cert-manager CRDs must already exist on the cluster. The operator creates Certificate resources referencing a user-specified Issuer or ClusterIssuer.

```yaml
spec:
  tls:
    mode: spiffe          # or certManager
    spiffe:
      trustDomain: "carbide.local"
      className: "zero-trust-workload-identity-manager-spire"
    # -- or --
    certManager:
      issuerRef:
        name: "bmm-ca-issuer"
        kind: "ClusterIssuer"
```

---

## Deployment Profiles

### **Management Profile** (`profile: management`)

**Purpose:** Hub cluster for orchestration and control plane

**Components Deployed:**
- carbide-rest-site-manager (watches Site CRDs)
- carbide-rest-cloud-worker / carbide-rest-site-worker (Temporal workers)
- Temporal (workflow engine, deployed via Helm)
- Keycloak (authentication, tri-mode: managed/external/disabled)
- REST API (management API)
- PostgreSQL (databases: `forge`, `temporal`, `temporal_visibility`, `keycloak`)

**Use Case:** Central management cluster that orchestrates multiple sites

**Sample:**
```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-management
  namespace: carbide-mgmt
spec:
  profile: management
  version: "v1.0.0"

  network:
    domain: "bmm.example.com"

  tls:
    mode: spiffe
    spiffe:
      trustDomain: "carbide.local"

  infrastructure:
    postgresql:
      mode: managed
      version: "16"
      storage:
        size: 50Gi

  rest:
    enabled: true
    temporal:
      mode: managed
      chartVersion: "0.73.1"
    keycloak:
      mode: managed
      realm: "bmm"
    restAPI:
      port: 8080
```

---

### **Site Profile** (`profile: site`)

**Purpose:** Spoke cluster for bare metal provisioning

**Components Deployed:**
- carbide-api (provisioning API)
- DHCP server
- PXE boot server
- DNS server
- Vault (BMC credential storage)
- RLA (Rack Level Administration)
- PSM (Power Shelf Manager)
- PostgreSQL (databases: `carbide`, `forge`, `rla`, `psm`)

**Use Case:** Edge site that provisions bare metal servers

**Sample:**
```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-site-boston
  namespace: carbide-site-boston
spec:
  profile: site
  version: "v1.0.0"

  # Optional: Link to Site CRD in hub cluster
  siteRef:
    cluster: "https://management.k8s.example.com"
    namespace: "nvidia-carbide-mgmt"
    name: "boston-site"
    uuid: "site-boston-abc123"

  network:
    interface: "enp2s0"
    ip: "192.168.33.10"
    adminNetworkCIDR: "192.168.33.0/24"
    domain: "bmm.example.com"

  tls:
    mode: spiffe
    spiffe:
      trustDomain: "carbide.local"

  infrastructure:
    postgresql:
      mode: managed
      version: "16"
      storage:
        size: 20Gi

  core:
    api:
      port: 1079
      replicas: 2
    dhcp:
      enabled: true
    pxe:
      enabled: true
      storage:
        size: 100Gi
    dns:
      enabled: true
    vault:
      mode: managed
      version: "1.15.6"
      storage:
        size: 10Gi
    rla:
      enabled: true
      port: 50051
    psm:
      enabled: true
      port: 50051

  rest:
    enabled: false  # Site profiles don't deploy REST tier
```

---

### **Management-with-Site Profile** (`profile: management-with-site`)

**Purpose:** All-in-one deployment (management + site services)

**Components Deployed:**
- Everything from management profile
- Everything from site profile

**Databases:** `carbide`, `forge`, `rla`, `psm`, `temporal`, `temporal_visibility`, `keycloak`

**Use Case:**
- Single-cluster deployments
- Hub sites that also provision hardware
- Development/demo environments

---

## Database Model

PostgreSQL uses a per-user/per-database model. Each database has its own PostgreSQL user. The databases created depend on the deployment profile:

| Profile | Databases |
|---------|-----------|
| site | `carbide`, `forge`, `rla`, `psm` |
| management | `forge`, `temporal`, `temporal_visibility`, `keycloak` |
| management-with-site | All of the above |

For external PostgreSQL, credentials are provided via a `userSecrets` map, keyed by database user name:

```yaml
infrastructure:
  postgresql:
    mode: external
    connection:
      host: "pg.example.com"
      port: 5432
      sslMode: require
      userSecrets:
        carbide:
          name: "pg-carbide-secret"
          passwordKey: "password"
        forge:
          name: "pg-forge-secret"
          passwordKey: "password"
        rla:
          name: "pg-rla-secret"
          passwordKey: "password"
        psm:
          name: "pg-psm-secret"
          passwordKey: "password"
```

---

## Hub-and-Spoke Architecture

### **Hub Cluster (Management)**

1. User deploys `CarbideDeployment` with `profile: management`
2. Operator deploys management infrastructure
3. **site-manager** watches for Site CRDs
4. User creates org/provider/site via **BMM UI/REST API**
5. Site CRD created -- site-manager processes it
6. Site registered in database

### **Spoke Cluster (Site)**

1. User deploys `CarbideDeployment` with `profile: site` (via GitOps/kubectl)
2. Operator deploys site infrastructure
3. (Optional) `spec.siteRef` documents link to hub Site CRD
4. Site services ready for provisioning

### **Relationship**

```
Hub Cluster (management.k8s.example.com)
+-- CarbideDeployment (profile: management)
|   +-- site-manager watches Site CRDs
+-- Site CRD: boston-site
|   +-- spec.uuid: "site-boston-abc123"
|   +-- status.bootstrapstate: "RegistrationComplete"
+-- Database: org/provider/site records

Spoke Cluster (boston.k8s.example.com)
+-- CarbideDeployment (profile: site)
    +-- spec.siteRef.name: "boston-site"
    +-- spec.siteRef.uuid: "site-boston-abc123"
    +-- Infrastructure: carbide-api, DHCP, PXE, DNS, Vault, RLA, PSM
```

---

## Site CRD and SiteRef

### **Site CRD** (managed by users via REST API)

Site CRDs are created by users through the Carbide UI/REST API, NOT by the operator.

```yaml
apiVersion: forge.nvidia.io/v1
kind: Site
metadata:
  name: boston-site
  namespace: carbide-mgmt
spec:
  uuid: "site-boston-abc123"
  sitename: "Boston Site"
  provider: "ACME Infrastructure"
  fcorg: "ACME Corp"
status:
  # Managed by site-manager
  bootstrapstate: "RegistrationComplete"
```

### **SiteRef** (optional field in CarbideDeployment)

`spec.siteRef` is an **optional** field that documents the relationship between a spoke CarbideDeployment and its hub Site CRD:

```yaml
spec:
  siteRef:
    cluster: "https://management.k8s.example.com"  # Hub cluster (optional)
    namespace: "nvidia-carbide-mgmt"
    name: "boston-site"
    uuid: "site-boston-abc123"  # Should match Site CRD's spec.uuid
```

**Purpose:**
- Documents hub-spoke relationship
- Enables future automation (central controller)
- Improves observability
- NOT enforced by operator (informational only)

---

## Operator Responsibilities

### **What the Operator Does:**

- Deploy infrastructure based on profile (PostgreSQL, Temporal, Vault, etc.)
- Deploy site-manager for management profiles
- Deploy site services for site profiles (carbide-api, DHCP, PXE, DNS, Vault, RLA, PSM)
- Create per-service ServiceAccounts (`carbide-api`, `carbide-dhcp`, `carbide-dns`, `carbide-pxe`, `carbide-rla`, `carbide-psm`)
- Create TLS resources (ClusterSPIFFEIDs or cert-manager Certificates) based on TLS mode
- Generate dynamic Casbin RBAC policy based on enabled services
- Install Site CRD (from carbide-rest)
- Report deployment status via conditions
- Reconcile infrastructure changes

### **What the Operator Does Not Do:**

- Create organizations/providers/sites (users do this via Carbide UI/REST API)
- Watch Site CRD to create CarbideDeployments (users do this via GitOps)
- Manage multi-cluster deployments (future: central controller)
- Validate/enforce siteRef (it's documentation only)
- Provision bare metal servers (Temporal workflows do this)

---

## Workflow: Deploying a New Site

### **Step 1: Deploy Management Cluster**

```bash
# Deploy management infrastructure
kubectl apply -f carbidedeployment-management.yaml

# Wait for ready
kubectl wait --for=condition=Ready carbidedeployment/carbide-management \
  -n carbide-mgmt --timeout=10m

# Verify site-manager is running
kubectl get pods -n carbide-mgmt -l app=site-manager
```

### **Step 2: Create Org/Provider/Site via Carbide UI**

Users access Carbide UI and:
1. Create Organization: "ACME Corp"
2. Create Provider: "ACME Infrastructure"
3. Create Site: "Boston Site"
   - This creates a Site CRD in the management cluster
   - site-manager processes it and creates database records

### **Step 3: Deploy Site Cluster**

```bash
# Deploy site infrastructure (via GitOps or kubectl)
kubectl apply -f carbidedeployment-site-boston.yaml

# Wait for ready
kubectl wait --for=condition=Ready carbidedeployment/carbide-site-boston \
  -n carbide-site-boston --timeout=10m

# Verify site services
kubectl get pods -n carbide-site-boston
kubectl get daemonset -n carbide-site-boston  # DHCP, PXE
```

### **Step 4: Site is Operational**

- Site registered in management cluster database
- Provisioning infrastructure deployed in site cluster
- Ready to provision bare metal servers via Temporal workflows

---

## Component Tiers

### **Infrastructure Tier**
- PostgreSQL (per-user/per-database model)
- Always deployed (managed or external)

### **Core Tier**
- carbide-api (gRPC provisioning API)
- DHCP server (DaemonSet on provisioning network)
- PXE boot server (TFTP + HTTP)
- DNS server (for provisioned servers)
- Vault (BMC credential storage, managed via Helm or external)
- RLA (Rack Level Administration, gRPC on port 50051)
- PSM (Power Shelf Manager, gRPC on port 50051)
- Deployed for site and management-with-site profiles

### **REST Tier**
- Temporal (workflow engine, deployed via Helm chart)
- Keycloak (authentication, tri-mode: managed/external/disabled)
- REST API (management API)
- carbide-rest-site-manager (watches Site CRDs)
- carbide-rest-cloud-worker / carbide-rest-site-worker (Temporal workflow workers)
- Site Agent (optional, for spoke sites)
- Deployed for management and management-with-site profiles

---

## Core Tier Reconciliation Order

The core tier reconciler follows a strict ordering with dependency gates:

1. **ServiceAccounts** - Per-service: `carbide-api`, `carbide-dhcp`, `carbide-dns`, `carbide-pxe`, plus `carbide-rla` and `carbide-psm` if enabled
2. **TLS resources** - SPIFFE ClusterSPIFFEIDs or cert-manager Certificates, depending on `tls.mode`
3. **Config** - ConfigMaps (API config, Casbin policy, DNS config) + Secrets (database credentials)
4. **Migration** - Database schema migration
5. **API** - carbide-api Deployment + Service
6. **Network services** - DHCP, PXE, DNS (after API ready)
7. **Vault** - Helm install + init job (after API ready)
8. **RLA** - Deployment + Service (after API + Vault ready)
9. **PSM** - Deployment + Service (after API + Vault ready)

---

## Keycloak Tri-Mode

Keycloak supports three authentication modes:

- **`managed`** - Deploys Keycloak via the RHBK (Red Hat Build of Keycloak) operator. The operator manages the Keycloak instance lifecycle.
- **`external`** - Accepts any OIDC-compliant provider (not limited to Keycloak). Configure one or more providers via the `authProviders[]` list, each specifying `issuerURL`, `jwksURL`, `clientID`, and optional `clientSecretRef`.
- **`disabled`** - No authentication is deployed. Suitable for development or environments with external auth handled outside the operator.

```yaml
rest:
  keycloak:
    mode: external
    authProviders:
    - name: "corporate-idp"
      issuerURL: "https://idp.example.com/realms/corp"
      jwksURL: "https://idp.example.com/realms/corp/protocol/openid-connect/certs"
      clientID: "bmm-client"
      clientSecretRef:
        name: "idp-client-secret"
        key: "client-secret"
```

---

## Dynamic Casbin Policy

The Casbin RBAC policy ConfigMap is dynamically generated based on which services are enabled. The base policy always includes entries for DHCP, DNS, trusted certificates, and anonymous access. When RLA or PSM are enabled, the operator appends service-specific policy entries granting those services their own SPIFFE identity mapping and access rules.

---

## Status and Conditions

CarbideDeployment reports status via:

```yaml
status:
  phase: Ready  # Pending | Provisioning | Ready | Failed | Updating

  infrastructure:
    ready: true
    message: "All infrastructure components ready"
    components:
    - name: PostgreSQL
      ready: true

  core:
    ready: true
    message: "All core components ready"
    components:
    - name: API
      ready: true
    - name: DHCP
      ready: true
    - name: PXE
      ready: true
    - name: DNS
      ready: true
    - name: Vault
      ready: true
    - name: RLA
      ready: true
    - name: PSM
      ready: true

  rest:
    ready: true
    message: "All REST components ready"
    components:
    - name: Temporal
      ready: true
    - name: Keycloak
      ready: true
    - name: RestAPI
      ready: true
    - name: SiteManager
      ready: true

  conditions:
  - type: Ready
    status: "True"
    reason: "AllComponentsReady"
  - type: TLSReady
    status: "True"
    reason: "Available"
  - type: SPIFFEAvailable
    status: "True"
    reason: "Detected"
  - type: VaultReady
    status: "True"
    reason: "Ready"
  - type: RLAReady
    status: "True"
    reason: "Ready"
  - type: PSMReady
    status: "True"
    reason: "Ready"
```

### Condition Types

| Condition | Description |
|-----------|-------------|
| `Ready` | Overall deployment readiness |
| `InfrastructureReady` | Infrastructure tier is ready |
| `CoreReady` | Core tier is ready |
| `RestReady` | REST tier is ready |
| `PostgreSQLReady` | PostgreSQL is ready |
| `APIReady` | carbide-api is ready |
| `MigrationComplete` | Database migration is complete |
| `DHCPReady` | DHCP server is ready |
| `PXEReady` | PXE server is ready |
| `DNSReady` | DNS server is ready |
| `TemporalReady` | Temporal is ready |
| `KeycloakReady` | Keycloak is ready |
| `RestAPIReady` | REST API is ready |
| `TLSReady` | TLS backend is available |
| `SPIFFEAvailable` | SPIRE is detected on the cluster |
| `CertManagerAvailable` | cert-manager CRDs are detected |
| `VaultReady` | Vault is ready |
| `RLAReady` | RLA is ready |
| `PSMReady` | PSM is ready |
| `SiteManagerReady` | site-manager is ready |
| `CloudWorkerReady` | carbide-rest-cloud-worker is ready |
| `SiteWorkerReady` | carbide-rest-site-worker is ready |
| `SiteAgentReady` | site agent is ready |

---

## Future Enhancements

### **Central Controller (Multi-Cluster Mode)**

Future capability where a central operator watches Site CRDs and creates CarbideDeployments in remote clusters:

```
Management Cluster:
  User creates Site CRD
    |
  Central operator watches Site CRD
    |
  Central operator creates CarbideDeployment in remote cluster
    |
  Local operator deploys infrastructure
    |
  Status synced back to Site CRD
```

This requires:
- Multi-cluster kubeconfig configuration
- Site-to-cluster mapping
- Cross-cluster status synchronization

**Not implemented yet** - current architecture uses GitOps for spoke deployments.

---

## Key Decisions

### **Why no org/provider/site creation in operator?**

- Organizations and providers are **tenant/business concepts**
- Should be managed through proper UI/API with access control
- Operator should focus on infrastructure only
- Follows Kubernetes separation of concerns

### **Why Jobs removed (no SiteSetup/SiteRegistration)?**

- Jobs are one-shot, not reconciliation-based
- Operator should use continuous reconciliation
- Business entities managed out-of-band via REST API
- Cleaner, more maintainable code

### **Why SiteRef is optional?**

- Not all deployments are hub-spoke (could be standalone)
- Users may want to manage relationship separately
- Informational only, not enforced
- Enables future automation without breaking current deployments

### **Why per-service ServiceAccounts?**

- Enables fine-grained SPIFFE identity per service
- Each service gets its own ClusterSPIFFEID or cert-manager Certificate
- Casbin RBAC policy maps service identities to specific permissions
- Follows principle of least privilege

### **Why Helm-based Temporal instead of Temporal Operator?**

- Removes the Temporal Operator as a prerequisite
- Job-based Helm install is simpler and more predictable
- Operator controls the full lifecycle via `helm upgrade --install`
- No dependency on TemporalCluster CRD

---

## References

- **Site CRD Source:** `carbide-rest/deploy/kustomize/base/crds/site-crd.yaml`
- **site-manager Source:** `carbide-rest/site-manager/`
- **Temporal Helm Chart:** `https://go.temporal.io/helm-charts` (deployed via Job-based Helm install)
- **Keycloak Operator (RHBK):** Used for managed Keycloak deployments
- **PostgreSQL Operator (PGO):** Used for managed PostgreSQL deployments
- **SPIRE:** Required when `tls.mode: spiffe` (must be pre-installed)
- **cert-manager:** Required when `tls.mode: certManager` (CRDs must exist)

---

*Architecture v2.0 - Post-refactor with TLS modes, per-database model, Vault/RLA/PSM services, Helm-based Temporal, and Keycloak tri-mode*

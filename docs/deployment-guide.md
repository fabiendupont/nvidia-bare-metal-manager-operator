# Carbide Operator Deployment Guide

This guide covers deploying the Carbide Operator to Kubernetes or OpenShift clusters.

## Prerequisites

### Required Operators

The Carbide operator depends on the following third-party operators being pre-installed:

1. **Crunchy PostgreSQL Operator** (for managed PostgreSQL)
   - Install via OperatorHub or: `kubectl apply -k https://github.com/CrunchyData/postgres-operator-examples/kustomize/install`
   - Version: 5.6.0+
   - Provides: `PostgresCluster` CRD

2. **Red Hat Build of Keycloak Operator** (optional, for managed Keycloak)
   - Install via OperatorHub (OpenShift) or operator marketplace
   - Provides: `Keycloak`, `KeycloakRealmImport` CRDs
   - Not required if Keycloak mode is set to `disabled` or `external` (using an external OIDC provider)

> **Note:** You can skip installing operators for components you plan to run in "external" mode.

### TLS Backend Prerequisites

The operator supports two TLS backends for mTLS. Install the one matching your chosen `tls.mode`:

- **SPIFFE mode** (`tls.mode: spiffe`, the default):
  - SPIRE must be installed on the cluster with the CSI driver (`csi.spiffe.io`) available.
  - The SPIRE agent DaemonSet and the `spiffe-csi-driver` CSIDriver resource must be present before creating a CarbideDeployment.

- **cert-manager mode** (`tls.mode: certManager`):
  - cert-manager must be installed (version 1.12+).
  - The `Certificate`, `Issuer`, and `ClusterIssuer` CRDs must be available.
  - You must have a configured `Issuer` or `ClusterIssuer` to reference in `tls.certManager.issuerRef`.

### Cluster Requirements

- Kubernetes 1.28+ or OpenShift 4.14+
- StorageClass configured (for PVCs)
- kubectl or oc CLI installed
- Cluster admin permissions

## Installation Methods

### Method 1: Using kubectl with kustomize (Recommended)

1. **Install the CRD:**
```bash
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/carbide-operator/main/config/crd/bases/carbide.nvidia.com_carbidedeployments.yaml
```

2. **Deploy the operator:**
```bash
kubectl apply -k https://github.com/NVIDIA/bare-metal-manager-operator/config/default
```

3. **Verify operator is running:**
```bash
kubectl get pods -n carbide-operator-system
```

Expected output:
```
NAME                                                  READY   STATUS    RESTARTS   AGE
carbide-operator-controller-manager-xxxxxxxxx-xxxxx   1/1     Running   0          30s
```

### Method 2: Using make deploy (Development)

1. **Clone the repository:**
```bash
git clone https://github.com/NVIDIA/bare-metal-manager-operator.git
cd carbide-operator
```

2. **Build and push image (if needed):**
```bash
export IMG=<your-registry>/carbide-operator:latest
make docker-build docker-push IMG=$IMG
```

3. **Deploy operator:**
```bash
make deploy IMG=$IMG
```

### Method 3: Manual Installation

1. **Install CRD:**
```bash
kubectl apply -f config/crd/bases/carbide.nvidia.com_carbidedeployments.yaml
```

2. **Create operator namespace:**
```bash
kubectl create namespace carbide-operator-system
```

3. **Apply RBAC:**
```bash
kubectl apply -f config/rbac/
```

4. **Deploy operator:**
```bash
kubectl apply -f config/manager/manager.yaml
```

## Creating a CarbideDeployment

### Minimal Configuration (All Managed Components)

Create a file `carbide-minimal.yaml`:

```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-minimal
spec:
  profile: management-with-site
  version: "latest"

  tls:
    mode: spiffe
    spiffe:
      trustDomain: carbide.local

  network:
    interface: enp2s0
    ip: 192.168.33.10
    domain: carbide.local
    adminNetworkCIDR: 192.168.33.0/24

  infrastructure:
    postgresql:
      mode: managed
      version: "16"
      storage:
        size: 10Gi

  core:
    api:
      port: 1079
    dhcp:
      enabled: true
    pxe:
      enabled: true
      storage:
        size: 50Gi
    dns:
      enabled: true
    vault:
      mode: managed
    rla:
      enabled: true
    psm:
      enabled: true

  rest:
    temporal:
      mode: managed
    keycloak:
      mode: managed
      realm: bmm
    restAPI:
      port: 8388
```

Apply:
```bash
kubectl apply -f carbide-minimal.yaml
```

### Production Configuration with REST Tier

```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-production
spec:
  profile: management-with-site
  version: "latest"

  tls:
    mode: spiffe
    spiffe:
      trustDomain: bmm.example.com

  network:
    interface: enp2s0
    ip: 192.168.33.10
    domain: bmm.example.com
    adminNetworkCIDR: 192.168.33.0/24

  infrastructure:
    storageClass: fast-ssd
    postgresql:
      mode: managed
      version: "16"
      storage:
        size: 100Gi
      replicas: 3

  core:
    api:
      port: 1079
      replicas: 3
    dhcp:
      enabled: true
    pxe:
      enabled: true
      storage:
        size: 200Gi
    dns:
      enabled: true
    security:
      tlsEnabled: true
      rbacBypass: false
    vault:
      mode: managed
      storage:
        size: 10Gi
    rla:
      enabled: true
      replicas: 2
    psm:
      enabled: true
      replicas: 2

  rest:
    enabled: true
    temporal:
      mode: managed
      version: "1.22.0"
      replicas: 3
    keycloak:
      mode: managed
      realm: bmm
    restAPI:
      port: 8080
      nodePort: 30388
      replicas: 3
```

### External Components Configuration

For production deployments using existing infrastructure:

```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-external
spec:
  profile: site
  version: "latest"

  tls:
    mode: certManager
    certManager:
      issuerRef:
        name: bmm-ca-issuer
        kind: ClusterIssuer

  network:
    interface: enp2s0
    ip: 192.168.33.10
    domain: carbide.local
    adminNetworkCIDR: 192.168.33.0/24

  infrastructure:
    postgresql:
      mode: external
      connection:
        host: postgres.example.com
        port: 5432
        sslMode: require
        userSecrets:
          carbide:
            name: pg-carbide-creds
          forge:
            name: pg-forge-creds
          rla:
            name: pg-rla-creds
          psm:
            name: pg-psm-creds

  core:
    api:
      port: 1079
    dhcp:
      enabled: true
    pxe:
      enabled: true
      storage:
        size: 50Gi
    dns:
      enabled: true
    vault:
      mode: external
      address: https://vault.example.com:8200
      tokenSecretRef:
        name: vault-token
        key: token
    rla:
      enabled: true
    psm:
      enabled: true

  rest:
    keycloak:
      mode: external
      authProviders:
        - name: corporate-sso
          issuerURL: https://sso.example.com/realms/bmm
          jwksURL: https://sso.example.com/realms/bmm/protocol/openid-connect/certs
          clientID: carbide-api
          clientSecretRef:
            name: sso-client-secret
            key: secret
```

Each secret referenced in `userSecrets` must contain `username`, `password`, and `dbname` keys. For example:

```bash
kubectl create secret generic pg-carbide-creds \
  --from-literal=username=carbide \
  --from-literal=password=<password> \
  --from-literal=dbname=carbide
```

## Monitoring Deployment Status

### Check CarbideDeployment status:

```bash
kubectl get carbidedeployment
```

Expected output:
```
NAME               PHASE         INFRASTRUCTURE   CORE   REST   AGE
carbide-minimal        Provisioning  true             true   N/A    2m
```

### Detailed status:

```bash
kubectl describe carbidedeployment carbide-minimal
```

Look for `Status.Conditions`:
- `InfrastructureReady: True` - PostgreSQL is ready
- `CoreReady: True` - carbide-api and network services are ready
- `TLSReady: True` - TLS backend (SPIFFE or cert-manager) is configured and available
- `VaultReady: True` - Vault is ready (if configured)
- `RLAReady: True` - Rack Level Administration is ready (if enabled)
- `PSMReady: True` - Power Shelf Manager is ready (if enabled)
- `RestReady: True` - Temporal, Keycloak, REST API are ready (if enabled)
- `Ready: True` - All tiers are ready

Additional component-level conditions are also available:
- `PostgreSQLReady`, `APIReady`, `DHCPReady`, `PXEReady`, `DNSReady`
- `TemporalReady`, `KeycloakReady`, `RestAPIReady`, `SiteAgentReady`
- `SPIFFEAvailable`, `CertManagerAvailable`
- `MigrationComplete`

### Check component pods:

```bash
# Infrastructure tier
kubectl get pods -n carbide-operators

# Core tier
kubectl get pods -n bmm

# Check specific component readiness
kubectl get postgrescluster -n carbide-operators
kubectl get deployment carbide-api -n bmm
```

## Troubleshooting

### Operator not starting

1. Check operator logs:
```bash
kubectl logs -n carbide-operator-system deployment/carbide-operator-controller-manager
```

2. Verify RBAC permissions:
```bash
kubectl auth can-i create postgresclusters --as=system:serviceaccount:carbide-operator-system:carbide-operator-controller-manager
```

### CarbideDeployment stuck in "Provisioning" phase

1. Check deployment status:
```bash
kubectl describe carbidedeployment <name>
```

2. Check conditions to see which tier is failing:
```bash
kubectl get carbidedeployment <name> -o jsonpath='{.status.conditions}'
```

3. Common issues:

   **PostgreSQL operator not installed:**
   ```
   Error: failed to create PostgresCluster: no matches for kind "PostgresCluster"
   ```
   Solution: Install Crunchy PostgreSQL operator

   **StorageClass not found:**
   ```
   Error: StorageClass "lvms-vg1" not found
   ```
   Solution: Use an existing StorageClass or create one

   **Insufficient permissions:**
   ```
   Error: User cannot create resource "postgresclusters"
   ```
   Solution: Verify operator RBAC permissions

### TLS backend not ready

1. **SPIFFE CSI driver not found:**
   ```
   Condition: SPIFFEAvailable=False, Reason=CSIDriverNotFound
   ```
   Solution: Install SPIRE with the CSI driver. Verify the `csi.spiffe.io` CSIDriver resource exists:
   ```bash
   kubectl get csidriver csi.spiffe.io
   ```
   If missing, follow the SPIRE installation guide to deploy the SPIFFE CSI driver.

2. **cert-manager CRDs not found:**
   ```
   Condition: CertManagerAvailable=False, Reason=CRDsNotFound
   ```
   Solution: Install cert-manager:
   ```bash
   kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
   ```
   Verify the CRDs are available:
   ```bash
   kubectl get crd certificates.cert-manager.io
   ```

3. **cert-manager Issuer not ready:**
   ```
   Condition: TLSReady=False, Reason=IssuerNotReady
   ```
   Solution: Check that the referenced Issuer or ClusterIssuer exists and is in a Ready state:
   ```bash
   kubectl get clusterissuer <issuer-name> -o yaml
   ```

### Vault not ready

1. Check Vault pod status:
```bash
kubectl get pods -l app.kubernetes.io/name=vault -n bmm
```

2. For managed Vault, check the Helm release:
```bash
helm list -n bmm | grep vault
```

3. For external Vault, verify connectivity and the token secret:
```bash
kubectl get secret vault-token -n bmm -o yaml
```

### RLA or PSM not ready

1. Check the specific condition:
```bash
kubectl get carbidedeployment <name> -o jsonpath='{.status.conditions[?(@.type=="RLAReady")]}'
kubectl get carbidedeployment <name> -o jsonpath='{.status.conditions[?(@.type=="PSMReady")]}'
```

2. Check pod status and logs:
```bash
kubectl get pods -l app=rla -n bmm
kubectl logs -n bmm deployment/rla
kubectl get pods -l app=psm -n bmm
kubectl logs -n bmm deployment/psm
```

3. Verify the database credentials are correct (each service requires its own database user and secret).

### Components not ready

1. Check individual component status:
```bash
# PostgreSQL
kubectl get postgrescluster -n carbide-operators -o yaml

# Keycloak (if REST tier enabled)
kubectl get keycloak -n bmm -o yaml
```

2. Check component pods:
```bash
kubectl get pods -n carbide-operators
kubectl get pods -n bmm
```

3. Check pod logs:
```bash
kubectl logs -n bmm deployment/carbide-api
kubectl logs -n carbide-operators statefulset/carbide-postgres
```

## Upgrading

To upgrade the operator:

1. **Update CRDs:**
```bash
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/carbide-operator/main/config/crd/bases/carbide.nvidia.com_carbidedeployments.yaml
```

2. **Update operator deployment:**
```bash
kubectl apply -k https://github.com/NVIDIA/bare-metal-manager-operator/config/default
```

Or with custom image:
```bash
make deploy IMG=<new-image>
```

## Uninstallation

1. **Delete all CarbideDeployments:**
```bash
kubectl delete carbidedeployment --all
```

Wait for all resources to be cleaned up (operator uses owner references).

2. **Delete operator:**
```bash
kubectl delete -k https://github.com/NVIDIA/bare-metal-manager-operator/config/default
```

3. **Delete CRD (optional):**
```bash
kubectl delete crd carbidedeployments.carbide.nvidia.com
```

## Advanced Topics

### TLS Mode Configuration

The operator requires a TLS backend for mTLS between Carbide components. Configure it via the top-level `tls` field.

**SPIFFE mode (default):**

Uses SPIRE to issue SVID certificates to workloads via the SPIFFE CSI driver. This is the recommended mode for zero-trust environments.

```yaml
spec:
  tls:
    mode: spiffe
    spiffe:
      trustDomain: carbide.local
      helperImage: ghcr.io/nvidia/spiffe-helper:latest
      className: zero-trust-workload-identity-manager-spire
```

- `trustDomain`: The SPIFFE trust domain. Defaults to `carbide.local`.
- `helperImage`: The spiffe-helper sidecar image. Defaults to `ghcr.io/nvidia/spiffe-helper:latest`.
- `className`: The SPIRE class name used for `ClusterSPIFFEID` resources. Defaults to `zero-trust-workload-identity-manager-spire`.

**cert-manager mode:**

Uses cert-manager to issue TLS certificates from a configured Issuer or ClusterIssuer.

```yaml
spec:
  tls:
    mode: certManager
    certManager:
      issuerRef:
        name: bmm-ca-issuer
        kind: ClusterIssuer
        group: cert-manager.io  # optional, defaults to cert-manager.io
```

### Vault Configuration

Vault is used for BMC credential storage in site profile deployments. It can be deployed in managed or external mode.

**Managed mode:**

The operator deploys Vault via Helm into the deployment namespace.

```yaml
spec:
  core:
    vault:
      mode: managed
      version: "1.15.6"
      storage:
        size: 10Gi
      kvMountPath: secrets
```

**External mode:**

Point to an existing Vault instance. The referenced secret must contain a `token` key with a valid Vault token.

```yaml
spec:
  core:
    vault:
      mode: external
      address: https://vault.example.com:8200
      tokenSecretRef:
        name: vault-token
        key: token
      kvMountPath: secrets
```

### Custom Resource Limits

```yaml
spec:
  core:
    api:
      resources:
        limits:
          cpu: "2"
          memory: 4Gi
        requests:
          cpu: "1"
          memory: 2Gi
```

### Using Specific Image Registry

```yaml
spec:
  images:
    registry: ghcr.io/nvidia
    pullPolicy: IfNotPresent
    bmmCore: ghcr.io/nvidia/carbide-core:v1.0.0
    restAPI: ghcr.io/nvidia/carbide-rest-api:v1.0.0
    rla: ghcr.io/nvidia/carbide-rla:v1.0.0
    psm: ghcr.io/nvidia/carbide-psm:v1.0.0
```

### Hub and Spoke Deployment

**Hub (Central Management):**
```yaml
spec:
  profile: management
  rest:
    enabled: true
    temporal:
      mode: managed
      replicas: 3
    keycloak:
      mode: managed
    restAPI:
      port: 8080
      replicas: 3
```

**Spoke (Edge Site):**
```yaml
spec:
  profile: site
  rest:
    enabled: true
    temporal:
      mode: external
      endpoint: "temporal-frontend.hub.example.com:7233"
    keycloak:
      mode: external
      authProviders:
        - name: hub-sso
          issuerURL: "https://hub.example.com/auth/realms/bmm"
          jwksURL: "https://hub.example.com/auth/realms/bmm/protocol/openid-connect/certs"
          clientID: carbide-spoke
          clientSecretRef:
            name: hub-keycloak-client
            key: client-secret
    restAPI:
      port: 8080
      replicas: 1
    siteAgent:
      enabled: true
      hubTemporalEndpoint: "temporal-frontend.hub.example.com:7233"
```

## Support

For issues and questions:
- GitHub Issues: https://github.com/NVIDIA/bare-metal-manager-operator/issues
- Documentation: https://github.com/NVIDIA/bare-metal-manager-operator/docs

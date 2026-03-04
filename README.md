# Carbide Operator

A Kubernetes operator for deploying and managing NVIDIA Carbide on OpenShift and Kubernetes clusters.

## Overview

The Carbide Operator simplifies the deployment of Carbide infrastructure by replacing manual manifest deployment with declarative CRDs. It orchestrates the deployment of all Carbide components across three tiers:

- **Infrastructure Tier**: PostgreSQL (per-service user model via Crunchy PGO)
- **Core Tier**: carbide-api, DHCP, PXE, DNS, Vault, RLA, PSM
- **REST Tier** (optional): Temporal (via Helm), Keycloak (managed/external/disabled), REST API, Workers, Site Manager, Site Agent

## Key Features

### Dual-Mode Architecture

Each component supports two deployment modes:

- **Managed Mode** (default): Operator creates and manages resources
  - PostgreSQL via Crunchy PostgreSQL Operator (per-service user secrets)
  - Temporal via Helm chart
  - Keycloak via RHBK Operator
  - Vault via Helm chart

- **External Mode**: Connect to pre-provisioned components
  - Flexible for production deployments
  - Use existing infrastructure
  - Maximum control

### TLS Backend Selection

Two mutually exclusive TLS backends for mTLS between services:

- **SPIFFE/SPIRE** (`tls.mode: spiffe`): Uses SPIRE CSI driver and ClusterSPIFFEID resources. Default mode.
- **cert-manager** (`tls.mode: certManager`): Uses cert-manager Certificates and an Issuer/ClusterIssuer you provide.

### Keycloak Tri-Mode

Authentication supports three modes via `rest.keycloak.mode`:

- **managed**: Operator deploys Keycloak via RHBK Operator and creates realm imports.
- **external**: Bring your own OIDC provider(s) via `authProviders[]`.
- **disabled**: No authentication enforcement.

### Vault Support

BMC credential storage for site profiles:

- **managed**: Deployed via Helm chart. Operator runs install and init jobs.
- **external**: Point to an existing Vault instance with address and token secret.

### Hub and Spoke Support

- **Hub Deployment** (`profile: management`): Central management with full REST tier
- **Spoke Deployment** (`profile: site`): Edge sites connecting to central hub
- **Standalone** (`profile: management-with-site`): Single-site deployment with all tiers

### Native Operator Integration

Uses native Kubernetes operator CRDs instead of Helm, providing:
- Cleaner separation of concerns
- Better error handling via Kubernetes conditions
- True GitOps-friendly deployments
- Community best practices

## Quick Start

### Prerequisites

1. **Install required operators** (for managed mode):
```bash
# Crunchy PostgreSQL Operator (always required for managed PostgreSQL)
kubectl apply -k https://github.com/CrunchyData/postgres-operator-examples/kustomize/install

# SPIRE (for SPIFFE TLS mode) or cert-manager (for certManager TLS mode)
# Install one TLS backend before deploying BMM.

# Keycloak operator (only needed when rest.keycloak.mode is "managed")
```

2. **Verify cluster requirements**:
- Kubernetes 1.28+ or OpenShift 4.14+
- StorageClass configured
- Cluster admin permissions

### Install Operator

```bash
# Install CRD
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/carbide-operator/main/config/crd/bases/carbide.nvidia.com_carbidedeployments.yaml

# Deploy operator
kubectl apply -k https://github.com/NVIDIA/bare-metal-manager-operator/config/default

# Verify
kubectl get pods -n carbide-operator-system
```

### Deploy Carbide (Minimal)

```bash
cat <<EOF | kubectl apply -f -
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-minimal
spec:
  profile: management-with-site
  version: "latest"

  network:
    interface: enp2s0
    ip: 192.168.33.10
    adminNetworkCIDR: 192.168.33.0/24

  tls:
    mode: spiffe
    spiffe:
      trustDomain: carbide.local

  infrastructure:
    postgresql:
      mode: managed
      version: "16"
      storage:
        size: 10Gi
  core:
    api: {}
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
EOF
```

### Monitor Deployment

```bash
# Check deployment status
kubectl get carbidedeployment

# View detailed status
kubectl describe carbidedeployment carbide-minimal

# Check component pods
kubectl get pods -n carbide-operators  # Infrastructure
kubectl get pods -n bmm            # Core + REST
```

## Documentation

- [Deployment Guide](docs/deployment-guide.md) - Comprehensive deployment instructions
- [API Reference](docs/api-reference.md) - Complete CRD documentation (auto-generated)
- [Architecture](docs/architecture.md) - Operator design and architecture

## Development

### Prerequisites

- Go 1.25+
- Docker or Podman
- kubectl or oc
- kustomize

### Build Operator

```bash
# Clone repository
git clone https://github.com/NVIDIA/bare-metal-manager-operator.git
cd carbide-operator

# Build binary
make build

# Run tests
make test

# Build container image
make docker-build IMG=<registry>/carbide-operator:<tag>
```

### Verify Build

```bash
# Run verification script
./hack/verify-deployment.sh
```

### Local Development

```bash
# Install CRDs
make install

# Run operator locally (outside cluster)
make run

# Deploy to cluster
make deploy IMG=<registry>/carbide-operator:<tag>
```

### Running Tests

```bash
# Unit tests
make test

# Integration tests (requires envtest)
make test-integration

# Generate code and manifests
make generate manifests
```

## Architecture

### Controller Structure

```
CarbideDeploymentReconciler (Main)
├── InfrastructureReconciler
│   └── PostgreSQL (Managed/External)
├── CoreReconciler
│   ├── ConfigMaps & Secrets
│   ├── carbide-api Deployment
│   ├── DHCP DaemonSet
│   ├── PXE DaemonSet
│   ├── DNS DaemonSet
│   ├── Vault (Managed/External)
│   ├── RLA Deployment
│   └── PSM Deployment
└── RestReconciler (Optional)
    ├── Temporal (Managed via Helm / External)
    ├── Keycloak (Managed/External/Disabled)
    ├── REST API Deployment
    ├── Workers (Cloud Worker, Site Worker)
    ├── Site Manager Deployment
    └── Site Agent Deployment
```

### Deployment Phases

1. **TLS prerequisite check**: Detect SPIRE CSI driver (spiffe mode) or cert-manager CRDs (certManager mode)
2. **Infrastructure**: PostgreSQL (managed via Crunchy PGO or external)
3. **Core**: Config + Migration -> API -> Network Services (DHCP, PXE, DNS) -> Vault -> RLA -> PSM
4. **REST**: Temporal (Helm) & Keycloak -> REST API -> Workers -> Site Manager -> Site Agent

### CRD Status

```yaml
status:
  phase: Pending | Provisioning | Ready | Failed | Updating
  infrastructure:
    ready: true
    components:
      - name: PostgreSQL
        ready: true
  core:
    ready: true
    components:
      - name: API
        ready: true
      - name: DHCP
        ready: true
      - name: Vault
        ready: true
      - name: RLA
        ready: true
      - name: PSM
        ready: true
  rest:
    ready: true
    components:
      - name: Temporal
        ready: true
      - name: Keycloak
        ready: true
      - name: RestAPI
        ready: true
      - name: CloudWorker
        ready: true
      - name: SiteWorker
        ready: true
      - name: SiteManager
        ready: true
  conditions:
    - type: TLSReady
      status: "True"
    - type: InfrastructureReady
      status: "True"
    - type: CoreReady
      status: "True"
    - type: VaultReady
      status: "True"
    - type: RLAReady
      status: "True"
    - type: PSMReady
      status: "True"
    - type: RestReady
      status: "True"
    - type: Ready
      status: "True"
```

## Configuration Examples

### Production with HA

```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-ha
spec:
  profile: management-with-site
  tls:
    mode: spiffe
  infrastructure:
    postgresql:
      replicas: 3  # HA PostgreSQL
      storage:
        size: 100Gi
  core:
    api:
      replicas: 3  # HA carbide-api
    dhcp:
      enabled: true
    pxe:
      enabled: true
    dns:
      enabled: true
    vault:
      mode: managed
    rla:
      enabled: true
    psm:
      enabled: true
  rest:
    enabled: true
    temporal:
      replicas: 3  # HA Temporal
    keycloak:
      mode: managed
    restAPI:
      replicas: 3
```

### External Infrastructure

```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-external
spec:
  profile: management-with-site
  tls:
    mode: certManager
    certManager:
      issuerRef:
        name: bmm-issuer
        kind: ClusterIssuer
  infrastructure:
    postgresql:
      mode: external
      connection:
        host: postgresql.prod.svc
        port: 5432
        sslMode: require
        userSecrets:
          carbide:
            name: pg-carbide-credentials
          forge:
            name: pg-forge-credentials
          temporal:
            name: pg-temporal-credentials
          keycloak:
            name: pg-keycloak-credentials
          rla:
            name: pg-rla-credentials
          psm:
            name: pg-psm-credentials
```

### External Keycloak (OIDC)

```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-external-auth
spec:
  profile: management
  rest:
    enabled: true
    keycloak:
      mode: external
      authProviders:
        - name: corporate-idp
          issuerURL: "https://idp.example.com/realms/bmm"
          jwksURL: "https://idp.example.com/realms/bmm/protocol/openid-connect/certs"
          clientID: bmm-client
          clientSecretRef:
            name: idp-client-secret
    temporal:
      mode: managed
    restAPI: {}
```

### Spoke Site (Edge)

```yaml
apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: carbide-spoke
spec:
  profile: site
  tls:
    mode: spiffe
  infrastructure:
    postgresql:
      mode: managed
  core:
    api: {}
    dhcp:
      enabled: true
    pxe:
      enabled: true
    dns:
      enabled: true
    vault:
      mode: managed
    rla:
      enabled: true
    psm:
      enabled: true
  siteRef:
    namespace: bmm
    name: edge-site-01
```

## Operator Dependencies

The operator validates these operators are installed before creating managed components:

| Component | Operator | API Group | Notes |
|-----------|----------|-----------|-------|
| PostgreSQL | Crunchy PostgreSQL | postgres-operator.crunchydata.com | Required for managed PostgreSQL |
| Keycloak | RHBK | k8s.keycloak.org | Only when `rest.keycloak.mode: managed` |

TLS backend (one required):

| Backend | Component | Detection |
|---------|-----------|-----------|
| SPIFFE/SPIRE | SPIRE CSI driver | CSIDriver `csi.spiffe.io` must exist |
| cert-manager | cert-manager | Certificate CRD must exist |

## Troubleshooting

### Operator won't start

```bash
# Check operator logs
kubectl logs -n carbide-operator-system deployment/carbide-operator-controller-manager

# Verify RBAC
kubectl auth can-i create postgresclusters --as=system:serviceaccount:carbide-operator-system:carbide-operator-controller-manager
```

### TLS backend not detected

```bash
# For SPIFFE mode: verify SPIRE CSI driver is installed
kubectl get csidrivers | grep spiffe

# For certManager mode: verify cert-manager CRDs exist
kubectl get crd certificates.cert-manager.io
```

### PostgresCluster not found

```bash
# Install Crunchy PostgreSQL Operator
kubectl apply -k https://github.com/CrunchyData/postgres-operator-examples/kustomize/install
```

### StorageClass issues

```bash
# List available StorageClasses
kubectl get storageclass

# Specify in CRD
spec:
  infrastructure:
    storageClass: <your-storageclass>
```

### Vault not initializing

```bash
# Check Vault Helm install job
kubectl get jobs -n bmm | grep vault

# Check Vault init job logs
kubectl logs -n bmm job/bmm-vault-init
```

## Contributing

Contributions welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.

## Project Status

**Version**: v1alpha1
**Status**: Active Development
**Maintainer**: NVIDIA Carbide Team

### Implementation Status

- [x] CRD Definition (v1alpha1)
- [x] Infrastructure Reconciler (PostgreSQL with per-service user model)
- [x] Core Reconciler (API, DHCP, PXE, DNS)
- [x] Vault support (managed via Helm / external)
- [x] RLA Deployment
- [x] PSM Deployment
- [x] REST Reconciler (Temporal via Helm, Keycloak tri-mode, REST API, Workers, Site Manager, Site Agent)
- [x] TLS backend selection (SPIFFE/SPIRE or cert-manager)
- [x] Dual-mode support (Managed/External)
- [x] Keycloak tri-mode (managed/external/disabled)
- [x] Deployment profiles (management / site / management-with-site)
- [x] Native operator CRD builders
- [x] RBAC configuration
- [x] Sample CRs
- [x] Deployment manifests
- [x] Docker build
- [x] Deployment verification script
- [x] Deployment documentation
- [ ] Unit tests (in progress)
- [ ] Integration tests (planned)
- [ ] E2E tests (planned)

## Links

- GitHub: https://github.com/NVIDIA/bare-metal-manager-operator
- Documentation: https://github.com/NVIDIA/bare-metal-manager-operator/docs
- Issues: https://github.com/NVIDIA/bare-metal-manager-operator/issues

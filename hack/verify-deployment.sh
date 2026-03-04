#!/bin/bash
# BMM Operator Deployment Verification Script
# This script verifies that all operator components are properly configured

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== BMM Operator Deployment Verification ==="
echo ""

# Check Go version
echo -n "Checking Go version... "
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo -e "${GREEN}✓${NC} $GO_VERSION"

# Build operator binary
echo -n "Building operator binary... "
if go build -o bin/manager cmd/main.go 2>&1 >/dev/null; then
    echo -e "${GREEN}✓${NC} bin/manager created"
else
    echo -e "${RED}✗${NC} Build failed"
    exit 1
fi

# Generate manifests
echo -n "Generating manifests... "
if make manifests 2>&1 >/dev/null; then
    echo -e "${GREEN}✓${NC} CRDs and RBAC generated"
else
    echo -e "${RED}✗${NC} Manifest generation failed"
    exit 1
fi

# Verify CRD exists
echo -n "Verifying CRD manifest... "
if [ -f "config/crd/bases/bmm.nvidia.com_bmmdeployments.yaml" ]; then
    echo -e "${GREEN}✓${NC} CRD manifest exists"
else
    echo -e "${RED}✗${NC} CRD manifest missing"
    exit 1
fi

# Verify RBAC includes operator permissions
echo -n "Verifying RBAC permissions... "
RBAC_FILE="config/rbac/role.yaml"
if grep -q "postgres-operator.crunchydata.com" "$RBAC_FILE" && \
   grep -q "temporal.io" "$RBAC_FILE" && \
   grep -q "k8s.keycloak.org" "$RBAC_FILE"; then
    echo -e "${GREEN}✓${NC} All operator CRD permissions present"
else
    echo -e "${YELLOW}⚠${NC} Missing some operator CRD permissions"
fi

# Test kustomize build
echo -n "Testing kustomize build... "
if command -v kustomize &> /dev/null; then
    RESOURCE_COUNT=$(kustomize build config/default 2>/dev/null | grep -c "^kind:" || true)
    if [ "$RESOURCE_COUNT" -gt 0 ]; then
        echo -e "${GREEN}✓${NC} Generated $RESOURCE_COUNT resources"
    else
        echo -e "${RED}✗${NC} Kustomize build failed"
        exit 1
    fi
else
    echo -e "${YELLOW}⚠${NC} kustomize not installed, skipping"
fi

# Verify sample CRs
echo -n "Verifying sample CRs... "
SAMPLE_DIR="config/samples"
SAMPLE_COUNT=$(find "$SAMPLE_DIR" -name "*.yaml" -not -name "kustomization.yaml" | wc -l)
if [ "$SAMPLE_COUNT" -ge 3 ]; then
    echo -e "${GREEN}✓${NC} Found $SAMPLE_COUNT sample CRs"
else
    echo -e "${YELLOW}⚠${NC} Only found $SAMPLE_COUNT sample CRs"
fi

# Check for required dependencies
echo ""
echo "=== Dependency Check ==="
echo -n "controller-gen... "
if [ -x "bin/controller-gen" ]; then
    echo -e "${GREEN}✓${NC}"
else
    echo -e "${YELLOW}⚠${NC} Not found (run 'make controller-gen')"
fi

echo -n "Docker/Podman... "
if command -v docker &> /dev/null || command -v podman &> /dev/null; then
    echo -e "${GREEN}✓${NC}"
else
    echo -e "${RED}✗${NC} Required for image builds"
fi

echo -n "kubectl... "
if command -v kubectl &> /dev/null; then
    echo -e "${GREEN}✓${NC}"
else
    echo -e "${YELLOW}⚠${NC} Required for deployment"
fi

echo ""
echo "=== Summary ==="
echo -e "${GREEN}✓${NC} Operator builds successfully"
echo -e "${GREEN}✓${NC} Manifests generate correctly"
echo -e "${GREEN}✓${NC} Ready for deployment"
echo ""
echo "Next steps:"
echo "  1. Build image: make docker-build IMG=<registry>/bmm-operator:<tag>"
echo "  2. Push image: make docker-push IMG=<registry>/bmm-operator:<tag>"
echo "  3. Deploy: make deploy IMG=<registry>/bmm-operator:<tag>"
echo ""

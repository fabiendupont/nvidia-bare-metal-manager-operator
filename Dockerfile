# Build the manager binary
# Use Red Hat Go Toolset on UBI 9 for FIPS-validated OpenSSL linkage
FROM registry.access.redhat.com/ubi9/go-toolset:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build with CGO enabled for FIPS-validated OpenSSL linkage.
# The RHEL Go Toolset patches the standard library to use OpenSSL when FIPS mode
# is active on the host, providing FIPS 140-3 compliance without code changes.
# GOARCH is left empty so the binary matches the build platform.
USER root
RUN CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -tags strictfipsruntime -o manager ./cmd/main.go

# Runtime image: Red Hat STIG-hardened UBI 9
# Provides DISA STIG controls pre-applied, FIPS-ready crypto stack,
# and satisfies Red Hat Certified Operator container requirements.
# See: https://www.redhat.com/en/blog/introducing-red-hats-stig-hardened-ubi-nvidia-gpus-red-hat-openshift
FROM registry.access.redhat.com/ubi9/ubi-stig:latest

# Labels required by Red Hat certification
LABEL name="nvidia-carbide-operator" \
      vendor="NVIDIA" \
      version="0.1.0" \
      summary="NVIDIA Bare Metal Manager Operator" \
      description="Kubernetes operator for deploying and managing NVIDIA Carbide bare metal provisioning infrastructure." \
      io.k8s.display-name="NVIDIA Carbide Operator" \
      io.k8s.description="Kubernetes operator for deploying and managing NVIDIA Carbide bare metal provisioning infrastructure." \
      io.openshift.tags="nvidia,bare-metal,provisioning,operator" \
      com.redhat.component="nvidia-carbide-operator-container"

# Copy licenses (required by Red Hat certification)
COPY LICENSE /licenses/LICENSE

WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

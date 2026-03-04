# File-Based Catalog (FBC) image for OLM.
# Build: podman build -f catalog.Dockerfile -t ghcr.io/nvidia/nvidia-carbide-operator-catalog:v0.1.0 .
# Validate: opm validate catalog/
FROM quay.io/operator-framework/opm:latest
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]
ADD catalog /configs
RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]
LABEL operators.operatorframework.io.index.configs.v1=/configs

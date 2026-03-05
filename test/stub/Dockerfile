# Stub image for E2E testing — mimics Carbide service health endpoints.
# Supports: carbide-api, carbide-rla, carbide-psm, carbide-rest-api, workers
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY test/stub/main.go .
RUN CGO_ENABLED=0 go build -o stub-server .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/stub-server /usr/local/bin/stub-server
# Create symlinks so the stub responds to any service binary name
RUN ln -s /usr/local/bin/stub-server /usr/local/bin/carbide-api && \
    ln -s /usr/local/bin/stub-server /opt/carbide/carbide-api && \
    mkdir -p /opt/carbide && \
    ln -s /usr/local/bin/stub-server /app/rla && \
    ln -s /usr/local/bin/stub-server /app/psm && \
    ln -s /usr/local/bin/stub-server /usr/local/bin/carbide-rest && \
    ln -s /usr/local/bin/stub-server /opt/carbide/carbide && \
    mkdir -p /var/run/secrets/tls && \
    mkdir -p /etc/carbide

USER 65532:65532
ENTRYPOINT ["stub-server"]

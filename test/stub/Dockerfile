# Stub image for E2E testing — mimics Carbide service health endpoints.
# Auto-detects service from binary name and configures ports accordingly.
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

# Create symlinks for all Carbide service binary names
RUN mkdir -p /opt/carbide /app /usr/local/bin && \
    ln -s /usr/local/bin/stub-server /opt/carbide/carbide-api && \
    ln -s /usr/local/bin/stub-server /opt/carbide/carbide && \
    ln -s /usr/local/bin/stub-server /usr/local/bin/carbide-api && \
    ln -s /usr/local/bin/stub-server /usr/local/bin/carbide-dns && \
    ln -s /usr/local/bin/stub-server /usr/local/bin/carbide-rest && \
    ln -s /usr/local/bin/stub-server /app/rla && \
    ln -s /usr/local/bin/stub-server /app/psm

# Create dirs that services expect
RUN mkdir -p /var/run/secrets/tls /var/run/secrets/db \
             /etc/carbide /etc/forge/carbide-api /tmp/k8s-webhook-server

# Dummy cert files for services that check TLS paths
RUN touch /var/run/secrets/tls/tls.crt /var/run/secrets/tls/tls.key \
          /var/run/secrets/tls/ca.crt /var/run/secrets/db/ca.crt

USER 65532:65532
ENTRYPOINT ["stub-server"]

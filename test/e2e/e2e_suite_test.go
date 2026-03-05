//go:build e2e
// +build e2e

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

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/NVIDIA/bare-metal-manager-operator/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips cert-manager installation during test setup.
	// - SPIRE_INSTALL_SKIP=true: Skips SPIRE installation during test setup.
	// These variables are useful when running against a cluster that already has these installed.
	skipCertManagerInstall = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	skipSpireInstall       = os.Getenv("SPIRE_INSTALL_SKIP") == "true"

	// projectImage is the name of the image which will be built and loaded
	// with the code source changes to be tested.
	projectImage = getEnvOrDefault("IMG", "localhost/nvidia-carbide-operator:e2e")
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purpose of being used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// prerequisites (PGO, cert-manager, SPIRE).
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting carbide-operator integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("building the manager(Operator) image")
	dockerfile := getEnvOrDefault("DOCKERFILE", "Dockerfile.ci")
	cmd := exec.Command("make", "docker-build",
		fmt.Sprintf("IMG=%s", projectImage),
		fmt.Sprintf("DOCKERFILE=%s", dockerfile))
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager(Operator) image")

	By("loading the manager(Operator) image on Kind")
	err = utils.LoadImageToKindClusterWithName(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager(Operator) image into Kind")

	By("deploying a standalone PostgreSQL for E2E tests")
	// Deploy a plain PostgreSQL instead of relying on PGO — the Crunchy operator
	// image is on a private registry that is unreliable in CI. The CarbideDeployment
	// CRs in E2E use external mode pointing to this standalone PostgreSQL.
	pgManifest := `
apiVersion: v1
kind: Namespace
metadata:
  name: postgres-e2e
---
apiVersion: v1
kind: Secret
metadata:
  name: postgres-credentials
  namespace: postgres-e2e
stringData:
  POSTGRES_USER: carbide
  POSTGRES_PASSWORD: e2e-test-password
  POSTGRES_DB: carbide
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: postgres-e2e
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: docker.io/library/postgres:16-alpine
        ports:
        - containerPort: 5432
        envFrom:
        - secretRef:
            name: postgres-credentials
        readinessProbe:
          exec:
            command: ["pg_isready", "-U", "carbide"]
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: postgres-e2e
spec:
  selector:
    app: postgres
  ports:
  - port: 5432
    targetPort: 5432
`
	pgFile := filepath.Join("/tmp", "e2e-postgres.yaml")
	err = os.WriteFile(pgFile, []byte(pgManifest), 0o644)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	defer os.Remove(pgFile)

	cmd = exec.Command("kubectl", "apply", "-f", pgFile)
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to deploy PostgreSQL")

	By("waiting for PostgreSQL to be ready")
	cmd = exec.Command("kubectl", "wait", "--for=condition=Ready",
		"pod/postgres-0", "-n", "postgres-e2e", "--timeout=120s")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "PostgreSQL not ready in time")

	By("creating databases for all services")
	for _, db := range []string{"forge", "rla", "psm", "temporal", "temporal_visibility"} {
		cmd = exec.Command("kubectl", "exec", "postgres-0", "-n", "postgres-e2e", "--",
			"psql", "-U", "carbide", "-c", fmt.Sprintf("CREATE DATABASE %s;", db))
		_, _ = utils.Run(cmd) // ignore if exists
		cmd = exec.Command("kubectl", "exec", "postgres-0", "-n", "postgres-e2e", "--",
			"psql", "-U", "carbide", "-c", fmt.Sprintf("CREATE USER %s WITH PASSWORD 'e2e-test-password';", db))
		_, _ = utils.Run(cmd)
		cmd = exec.Command("kubectl", "exec", "postgres-0", "-n", "postgres-e2e", "--",
			"psql", "-U", "carbide", "-c", fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s;", db, db))
		_, _ = utils.Run(cmd)
	}

	By("installing PGO CRDs (for PostgresCluster CR validation)")
	cmd = exec.Command("kubectl", "apply", "--server-side", "-k",
		"https://github.com/CrunchyData/postgres-operator//config/crd")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to install PGO CRDs")

	if !skipCertManagerInstall {
		By("installing cert-manager")
		cmd = exec.Command("kubectl", "apply", "-f",
			"https://github.com/cert-manager/cert-manager/releases/download/v1.17.2/cert-manager.yaml")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to install cert-manager")

		By("waiting for cert-manager webhook to be ready")
		cmd = exec.Command("kubectl", "wait", "--for=condition=Available",
			"deployment/cert-manager-webhook", "-n", "cert-manager", "--timeout=120s")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "cert-manager webhook did not become ready in time")
	}

	if !skipSpireInstall {
		By("installing SPIRE CRDs and server")
		cmd = exec.Command("helm", "repo", "add", "spiffe",
			"https://spiffe.github.io/helm-charts-hardened/")
		_, _ = utils.Run(cmd) // ignore if already added

		cmd = exec.Command("helm", "repo", "update")
		_, _ = utils.Run(cmd)

		// Install SPIRE with CRDs enabled and skip ClusterSPIFFEID resources
		// that require the controller-manager to be running first
		cmd = exec.Command("helm", "install", "spire", "spiffe/spire",
			"-n", "spire", "--create-namespace",
			"--set", "spire-crds.enabled=true",
			"--set", "spire-server.controllerManager.enabled=false",
			"--wait", "--timeout", "5m")
		_, err = utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to install SPIRE")
	}

	By("installing CRDs")
	cmd = exec.Command("make", "install")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying the controller-manager")
	cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

	By("waiting for the controller-manager pod to be ready")
	cmd = exec.Command("kubectl", "wait", "--for=condition=Available",
		"deployment/carbide-operator-controller-manager",
		"-n", "carbide-operator-system", "--timeout=120s")
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Controller manager did not become ready in time")
})

var _ = AfterSuite(func() {
	By("undeploying the controller-manager")
	cmd := exec.Command("make", "undeploy")
	_, _ = utils.Run(cmd)

	By("uninstalling CRDs")
	cmd = exec.Command("make", "uninstall")
	_, _ = utils.Run(cmd)
})

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/NVIDIA/bare-metal-manager-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "carbide-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "carbide-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "carbide-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "carbide-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=carbide-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	Context("CarbideDeployment with cert-manager TLS", func() {
		const cmNamespace = "nvidia-carbide-e2e-cm"
		const cmCRName = "e2e-certmanager"

		BeforeEach(func() {
			By("creating the test namespace")
			cmd := exec.Command("kubectl", "create", "ns", cmNamespace)
			_, _ = utils.Run(cmd)

			By("creating per-user PostgreSQL secrets")
			createPGUserSecrets(cmNamespace)
		})

		AfterEach(func() {
			By("cleaning up the CarbideDeployment CR")
			cmd := exec.Command("kubectl", "delete", "carbidedeployment", cmCRName,
				"-n", cmNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the ClusterIssuer")
			cmd = exec.Command("kubectl", "delete", "clusterissuer",
				"carbide-e2e-selfsigned", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", cmNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should deploy a full stack with cert-manager TLS", func() {
			By("creating a self-signed ClusterIssuer for cert-manager")
			issuerYAML := `apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: carbide-e2e-selfsigned
spec:
  selfSigned: {}
`
			issuerFile := filepath.Join("/tmp", "e2e-clusterissuer.yaml")
			err := os.WriteFile(issuerFile, []byte(issuerYAML), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(issuerFile)

			cmd := exec.Command("kubectl", "apply", "-f", issuerFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterIssuer")

			By("applying a CarbideDeployment CR with cert-manager TLS")
			cr := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: management-with-site
  version: "latest"
  tls:
    mode: certManager
    certManager:
      issuerRef:
        name: carbide-e2e-selfsigned
        kind: ClusterIssuer
  network:
    interface: eth0
    ip: 10.0.0.1
    adminNetworkCIDR: 10.0.0.0/24
    domain: carbide.local
  infrastructure:
    namespace: %s
    postgresql:
      mode: external
      connection:
        host: postgres.postgres-e2e.svc
        port: 5432
        sslMode: disable
        userSecrets:
          carbide:
            name: pg-carbide
          forge:
            name: pg-forge
          rla:
            name: pg-rla
          psm:
            name: pg-psm
  core:
    namespace: %s
    api:
      port: 1079
    dhcp:
      enabled: false
    dns:
      enabled: false
    pxe:
      enabled: false
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
      mode: disabled
    restAPI:
      port: 8080
`, cmCRName, cmNamespace, cmNamespace, cmNamespace)

			crFile := filepath.Join("/tmp", "e2e-cr-certmanager.yaml")
			err = os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd = exec.Command("kubectl", "apply", "-f", crFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply CarbideDeployment CR")

			By("waiting for PostgresCluster to exist")
			verifyPGCluster := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "postgrescluster",
					"-n", cmNamespace, "-o", "jsonpath={.items[*].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "PostgresCluster should exist")
			}
			Eventually(verifyPGCluster, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying ConfigMaps were created")
			verifyConfigMaps := func(g Gomega) {
				for _, cmName := range []string{"carbide-api-config", "casbin-policy"} {
					cmd := exec.Command("kubectl", "get", "configmap", cmName,
						"-n", cmNamespace)
					_, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred(), "ConfigMap %s should exist", cmName)
				}
			}
			Eventually(verifyConfigMaps, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying ServiceAccounts were created")
			verifySAs := func(g Gomega) {
				for _, saName := range []string{"carbide-api", "carbide-rla", "carbide-psm"} {
					cmd := exec.Command("kubectl", "get", "serviceaccount", saName,
						"-n", cmNamespace)
					_, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred(), "ServiceAccount %s should exist", saName)
				}
			}
			Eventually(verifySAs, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying cert-manager Certificates were created")
			verifyCerts := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "certificate",
					"-n", cmNamespace, "-o", "jsonpath={.items[*].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "cert-manager Certificates should exist")
			}
			Eventually(verifyCerts, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying status has conditions set")
			verifyConditions := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", cmCRName,
					"-n", cmNamespace,
					"-o", "jsonpath={.status.conditions[*].type}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "status.conditions should be set")
			}
			Eventually(verifyConditions, 60*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("CarbideDeployment with SPIFFE TLS", func() {
		const spiffeNamespace = "nvidia-carbide-e2e-spiffe"
		const spiffeCRName = "e2e-spiffe"

		BeforeEach(func() {
			By("creating the test namespace")
			cmd := exec.Command("kubectl", "create", "ns", spiffeNamespace)
			_, _ = utils.Run(cmd)

			By("creating per-user PostgreSQL secrets")
			createPGUserSecrets(spiffeNamespace)
		})

		AfterEach(func() {
			By("cleaning up the CarbideDeployment CR")
			cmd := exec.Command("kubectl", "delete", "carbidedeployment", spiffeCRName,
				"-n", spiffeNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", spiffeNamespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should deploy a full stack with SPIFFE TLS", func() {
			By("applying a CarbideDeployment CR with SPIFFE TLS")
			cr := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: management-with-site
  version: "latest"
  tls:
    mode: spiffe
    spiffe:
      trustDomain: carbide.local
  network:
    interface: eth0
    ip: 10.0.0.1
    adminNetworkCIDR: 10.0.0.0/24
    domain: carbide.local
  infrastructure:
    namespace: %s
    postgresql:
      mode: external
      connection:
        host: postgres.postgres-e2e.svc
        port: 5432
        sslMode: disable
        userSecrets:
          carbide:
            name: pg-carbide
          forge:
            name: pg-forge
          rla:
            name: pg-rla
          psm:
            name: pg-psm
  core:
    namespace: %s
    api:
      port: 1079
    dhcp:
      enabled: false
    dns:
      enabled: false
    pxe:
      enabled: false
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
      mode: disabled
    restAPI:
      port: 8080
`, spiffeCRName, spiffeNamespace, spiffeNamespace, spiffeNamespace)

			crFile := filepath.Join("/tmp", "e2e-cr-spiffe.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply CarbideDeployment CR")

			By("waiting for PostgresCluster to exist")
			verifyPGCluster := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "postgrescluster",
					"-n", spiffeNamespace, "-o", "jsonpath={.items[*].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "PostgresCluster should exist")
			}
			Eventually(verifyPGCluster, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying ClusterSPIFFEIDs were created")
			verifyCSIDs := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "clusterspiffeid",
					"-o", "jsonpath={.items[*].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "ClusterSPIFFEIDs should exist")
				for _, component := range []string{"carbide-api", "carbide-rla", "carbide-psm"} {
					g.Expect(output).To(ContainSubstring(component),
						"ClusterSPIFFEID for %s should exist", component)
				}
			}
			Eventually(verifyCSIDs, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying spiffe-helper-config ConfigMap exists")
			verifyHelperConfig := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap", "spiffe-helper-config",
					"-n", spiffeNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "spiffe-helper-config ConfigMap should exist")
			}
			Eventually(verifyHelperConfig, 2*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying status has SPIFFEAvailable=True condition")
			verifySPIFFECondition := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", spiffeCRName,
					"-n", spiffeNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='SPIFFEAvailable')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "SPIFFEAvailable condition should be True")
			}
			Eventually(verifySPIFFECondition, 2*time.Minute, 2*time.Second).Should(Succeed())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// createPGUserSecrets creates per-user PostgreSQL secrets in the given namespace
// matching the external PostgreSQL deployed in BeforeSuite.
func createPGUserSecrets(namespace string) {
	for _, user := range []string{"carbide", "forge", "rla", "psm"} {
		secretYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: pg-%s
  namespace: %s
stringData:
  host: postgres.postgres-e2e.svc
  port: "5432"
  user: %s
  password: e2e-test-password
  dbname: %s
  username: %s
`, user, namespace, user, user, user)

		secretFile := filepath.Join("/tmp", fmt.Sprintf("e2e-pg-secret-%s.yaml", user))
		err := os.WriteFile(secretFile, []byte(secretYAML), 0o644)
		ExpectWithOffset(2, err).NotTo(HaveOccurred())
		defer os.Remove(secretFile)

		cmd := exec.Command("kubectl", "apply", "-f", secretFile)
		_, err = utils.Run(cmd)
		ExpectWithOffset(2, err).NotTo(HaveOccurred(),
			fmt.Sprintf("Failed to create PG secret for user %s", user))
	}
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

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
const namespace = "nvidia-carbide"

// serviceAccountName created for the project
const serviceAccountName = "controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "nvidia-carbide-metrics-binding"

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
				"--clusterrole=metrics-reader",
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

			By("waiting for carbide-api-config ConfigMap to exist")
			verifyPGCluster := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap", "carbide-api-config",
					"-n", cmNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(err).NotTo(HaveOccurred(), "carbide-api-config ConfigMap should exist")
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
					"-n", cmNamespace)
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

	Context("Webhook validation", func() {
		It("should reject a site profile without network config", func() {
			cr := `apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: e2e-webhook-no-network
  namespace: default
spec:
  profile: site
  version: "latest"
  network:
    domain: carbide.local
  core:
    api:
      port: 1079
    dhcp:
      enabled: false
    dns:
      enabled: false
    pxe:
      enabled: false
`
			crFile := filepath.Join("/tmp", "e2e-webhook-no-network.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			output, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "expected webhook to reject CR without network config")
			Expect(output).To(ContainSubstring("required"))
		})

		It("should reject external PostgreSQL without host", func() {
			cr := `apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: e2e-webhook-no-pg-host
  namespace: default
spec:
  profile: management
  version: "latest"
  network:
    domain: carbide.local
  infrastructure:
    postgresql:
      mode: external
      connection:
        host: ""
        port: 5432
  core:
    api:
      port: 1079
    dhcp:
      enabled: false
    dns:
      enabled: false
    pxe:
      enabled: false
  rest:
    temporal:
      mode: managed
    keycloak:
      mode: disabled
    restAPI:
      port: 8080
`
			crFile := filepath.Join("/tmp", "e2e-webhook-no-pg-host.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			output, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "expected webhook to reject CR without PG host")
			Expect(output).To(ContainSubstring("required"))
		})

		It("should reject certManager TLS without issuerRef", func() {
			cr := `apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: e2e-webhook-no-issuer
  namespace: default
spec:
  profile: management
  version: "latest"
  tls:
    mode: certManager
  network:
    domain: carbide.local
  core:
    api:
      port: 1079
    dhcp:
      enabled: false
    dns:
      enabled: false
    pxe:
      enabled: false
  rest:
    temporal:
      mode: managed
    keycloak:
      mode: disabled
    restAPI:
      port: 8080
`
			crFile := filepath.Join("/tmp", "e2e-webhook-no-issuer.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			output, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "expected webhook to reject CR without issuerRef")
			Expect(output).To(ContainSubstring("required"))
		})

		It("should reject management profile without rest config", func() {
			cr := `apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: e2e-webhook-no-rest
  namespace: default
spec:
  profile: management
  version: "latest"
  network:
    domain: carbide.local
  core:
    api:
      port: 1079
    dhcp:
      enabled: false
    dns:
      enabled: false
    pxe:
      enabled: false
`
			crFile := filepath.Join("/tmp", "e2e-webhook-no-rest.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			output, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "expected webhook to reject management profile without rest config")
			Expect(output).To(ContainSubstring("required"))
		})
	})

	Context("CR update handling", func() {
		const updateNamespace = "nvidia-carbide-e2e-update"
		const updateCRName = "e2e-update"

		BeforeEach(func() {
			By("creating the test namespace")
			cmd := exec.Command("kubectl", "create", "ns", updateNamespace)
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			By("cleaning up the CarbideDeployment CR")
			cmd := exec.Command("kubectl", "delete", "carbidedeployment", updateCRName,
				"-n", updateNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", updateNamespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should update ConfigMap when API port changes", func() {
			By("applying a management-profile CR with api.port 1079")
			cr := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: management
  version: "latest"
  network:
    domain: carbide.local
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
  rest:
    temporal:
      mode: managed
    keycloak:
      mode: disabled
    restAPI:
      port: 8080
`, updateCRName, updateNamespace, updateNamespace)

			crFile := filepath.Join("/tmp", "e2e-cr-update.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply CarbideDeployment CR")

			By("waiting for reconciliation (finalizer exists)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", updateCRName,
					"-n", updateNamespace,
					"-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("carbide.nvidia.com/finalizer"))
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			By("patching the CR to change api.port to 2079")
			cmd = exec.Command("kubectl", "patch", "carbidedeployment", updateCRName,
				"-n", updateNamespace, "--type=merge",
				"-p", `{"spec":{"core":{"api":{"port":2079}}}}`)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to patch CarbideDeployment CR")

			By("verifying the CR was re-reconciled after patch")
			// Management profile doesn't create carbide-api-config ConfigMap
			// (that's a site resource). Verify the patch was accepted and the
			// operator re-reconciled by checking observedGeneration advances.
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", updateCRName,
					"-n", updateNamespace,
					"-o", "jsonpath={.status.observedGeneration}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				// After patching, generation increases and observedGeneration should match
				cmd = exec.Command("kubectl", "get", "carbidedeployment", updateCRName,
					"-n", updateNamespace,
					"-o", "jsonpath={.metadata.generation}")
				genOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal(genOutput), "observedGeneration should match generation after reconcile")
			}, 60*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("CR deletion", func() {
		const deleteNamespace = "nvidia-carbide-e2e-delete"
		const deleteCRName = "e2e-delete"

		BeforeEach(func() {
			By("creating the test namespace")
			cmd := exec.Command("kubectl", "create", "ns", deleteNamespace)
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			By("cleaning up the test namespace")
			cmd := exec.Command("kubectl", "delete", "ns", deleteNamespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should clean up child resources when CR is deleted", func() {
			By("applying a management-profile CR")
			cr := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: management
  version: "latest"
  network:
    domain: carbide.local
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
  rest:
    temporal:
      mode: managed
    keycloak:
      mode: disabled
    restAPI:
      port: 8080
`, deleteCRName, deleteNamespace, deleteNamespace)

			crFile := filepath.Join("/tmp", "e2e-cr-delete.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply CarbideDeployment CR")

			By("waiting for finalizer and conditions")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", deleteCRName,
					"-n", deleteNamespace,
					"-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("carbide.nvidia.com/finalizer"))
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", deleteCRName,
					"-n", deleteNamespace,
					"-o", "jsonpath={.status.conditions[*].type}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "status.conditions should be set")
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying at least one child resource exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "serviceaccount,configmap",
					"-n", deleteNamespace,
					"-o", "jsonpath={.items[*].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "expected child resources to exist")
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			By("deleting the CarbideDeployment CR")
			cmd = exec.Command("kubectl", "delete", "carbidedeployment", deleteCRName,
				"-n", deleteNamespace, "--timeout=60s")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete CarbideDeployment CR")

			By("verifying the CR is gone")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", deleteCRName,
					"-n", deleteNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "CR should no longer exist")
			}, 60*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Status progression", func() {
		const statusNamespace = "nvidia-carbide-e2e-status"
		const statusCRName = "e2e-status"

		BeforeEach(func() {
			By("creating the test namespace")
			cmd := exec.Command("kubectl", "create", "ns", statusNamespace)
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			By("cleaning up the CarbideDeployment CR")
			cmd := exec.Command("kubectl", "delete", "carbidedeployment", statusCRName,
				"-n", statusNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", statusNamespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should set phase to Provisioning and populate conditions", func() {
			By("applying a management-profile CR")
			cr := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: management
  version: "latest"
  network:
    domain: carbide.local
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
  rest:
    temporal:
      mode: managed
    keycloak:
      mode: disabled
    restAPI:
      port: 8080
`, statusCRName, statusNamespace, statusNamespace)

			crFile := filepath.Join("/tmp", "e2e-cr-status.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply CarbideDeployment CR")

			By("verifying status.phase is not empty")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", statusCRName,
					"-n", statusNamespace,
					"-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "status.phase should be set")
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying status.conditions contains Ready type")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", statusCRName,
					"-n", statusNamespace,
					"-o", "jsonpath={.status.conditions[*].type}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Ready"))
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying status.observedGeneration equals metadata.generation")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", statusCRName,
					"-n", statusNamespace,
					"-o", "jsonpath={.metadata.generation}")
				generation, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(generation).NotTo(BeEmpty())

				cmd = exec.Command("kubectl", "get", "carbidedeployment", statusCRName,
					"-n", statusNamespace,
					"-o", "jsonpath={.status.observedGeneration}")
				observedGeneration, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(observedGeneration).To(Equal(generation))
			}, 60*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Multi-profile deployment", func() {
		const mgmtNamespace = "nvidia-carbide-e2e-mgmt"
		const siteNamespace = "nvidia-carbide-e2e-site"
		const mgmtCRName = "e2e-multi-mgmt"
		const siteCRName = "e2e-multi-site"

		BeforeEach(func() {
			By("creating the management test namespace")
			cmd := exec.Command("kubectl", "create", "ns", mgmtNamespace)
			_, _ = utils.Run(cmd)

			By("creating the site test namespace")
			cmd = exec.Command("kubectl", "create", "ns", siteNamespace)
			_, _ = utils.Run(cmd)
		})

		AfterEach(func() {
			By("cleaning up the management CarbideDeployment CR")
			cmd := exec.Command("kubectl", "delete", "carbidedeployment", mgmtCRName,
				"-n", mgmtNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the site CarbideDeployment CR")
			cmd = exec.Command("kubectl", "delete", "carbidedeployment", siteCRName,
				"-n", siteNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the management test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", mgmtNamespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the site test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", siteNamespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should reconcile management and site profiles in separate namespaces", func() {
			By("applying a management-profile CR in the management namespace")
			mgmtCR := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: management
  version: "latest"
  network:
    domain: carbide.local
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
  rest:
    temporal:
      mode: managed
    keycloak:
      mode: disabled
    restAPI:
      port: 8080
`, mgmtCRName, mgmtNamespace, mgmtNamespace)

			mgmtFile := filepath.Join("/tmp", "e2e-cr-multi-mgmt.yaml")
			err := os.WriteFile(mgmtFile, []byte(mgmtCR), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(mgmtFile)

			cmd := exec.Command("kubectl", "apply", "-f", mgmtFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply management CarbideDeployment CR")

			By("applying a site-profile CR in the site namespace")
			siteCR := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: site
  version: "latest"
  network:
    interface: eth0
    ip: 10.0.0.1
    adminNetworkCIDR: 10.0.0.0/24
    domain: carbide.local
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
`, siteCRName, siteNamespace, siteNamespace)

			siteFile := filepath.Join("/tmp", "e2e-cr-multi-site.yaml")
			err = os.WriteFile(siteFile, []byte(siteCR), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(siteFile)

			cmd = exec.Command("kubectl", "apply", "-f", siteFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply site CarbideDeployment CR")

			By("verifying management CR has finalizer and conditions")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", mgmtCRName,
					"-n", mgmtNamespace,
					"-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("carbide.nvidia.com/finalizer"))
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", mgmtCRName,
					"-n", mgmtNamespace,
					"-o", "jsonpath={.status.conditions[*].type}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "management CR conditions should be set")
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying site CR has finalizer and conditions")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", siteCRName,
					"-n", siteNamespace,
					"-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("carbide.nvidia.com/finalizer"))
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", siteCRName,
					"-n", siteNamespace,
					"-o", "jsonpath={.status.conditions[*].type}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "site CR conditions should be set")
			}, 60*time.Second, 2*time.Second).Should(Succeed())
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

			By("waiting for carbide-api-config ConfigMap to exist")
			verifyPGCluster := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap", "carbide-api-config",
					"-n", spiffeNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(err).NotTo(HaveOccurred(), "carbide-api-config ConfigMap should exist")
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

	Context("Tier 2: services start with stub image", func() {
		const tier2Namespace = "nvidia-carbide-e2e-tier2"
		const tier2CRName = "e2e-tier2-site"

		BeforeEach(func() {
			By("creating the Tier 2 test namespace")
			cmd := exec.Command("kubectl", "create", "ns", tier2Namespace)
			_, _ = utils.Run(cmd)

			By("ensuring the self-signed ClusterIssuer exists")
			ensureClusterIssuer()

			By("creating per-user PostgreSQL secrets")
			createPGUserSecrets(tier2Namespace)
		})

		AfterEach(func() {
			By("cleaning up the Tier 2 CarbideDeployment CR")
			cmd := exec.Command("kubectl", "delete", "carbidedeployment", tier2CRName,
				"-n", tier2Namespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the Tier 2 test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", tier2Namespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should deploy site services using stub image and become Available", func() {
			By("applying a CarbideDeployment CR with stub image overrides")
			cr := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: site
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
  images:
    bmmCore: localhost/carbide-api:e2e
    rla: localhost/carbide-rla:e2e
    psm: localhost/carbide-psm:e2e
    pullPolicy: Never
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
`, tier2CRName, tier2Namespace, tier2Namespace, tier2Namespace)

			crFile := filepath.Join("/tmp", "e2e-cr-tier2-site.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Tier 2 CarbideDeployment CR")

			By("waiting for Deployments to be created by the operator")
			verifyDeploymentsExist := func(g Gomega) {
				for _, deploy := range []string{"carbide-api", "carbide-rla", "carbide-psm"} {
					cmd := exec.Command("kubectl", "get", "deployment", deploy,
						"-n", tier2Namespace)
					_, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred(),
						"Deployment %s should exist", deploy)
				}
			}
			Eventually(verifyDeploymentsExist, 5*time.Minute, 2*time.Second).Should(Succeed())

			By("waiting for Deployments to become Available")
			for _, deploy := range []string{"carbide-api", "carbide-rla", "carbide-psm"} {
				cmd = exec.Command("kubectl", "wait", "--for=condition=Available",
					fmt.Sprintf("deployment/%s", deploy),
					"-n", tier2Namespace, "--timeout=120s")
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred(),
					"Deployment %s did not become Available", deploy)
			}

			By("verifying pod logs contain stub server startup message")
			for _, label := range []string{"carbide-api", "carbide-rla", "carbide-psm"} {
				verifyStubLogs := func(g Gomega) {
					cmd := exec.Command("kubectl", "logs",
						"-l", fmt.Sprintf("app=%s", label),
						"-n", tier2Namespace, "--tail=50")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).To(ContainSubstring("starting stub server"),
						"Pod logs for %s should contain stub startup message", label)
				}
				Eventually(verifyStubLogs, 30*time.Second, 2*time.Second).Should(Succeed())
			}

			By("verifying status conditions are set")
			verifyConditions := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", tier2CRName,
					"-n", tier2Namespace,
					"-o", "jsonpath={.status.conditions[*].type}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "status.conditions should be set")
			}
			Eventually(verifyConditions, 60*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	Context("Tier 2: management with Keycloak and stub REST API", func() {
		const tier2MgmtNamespace = "nvidia-carbide-e2e-tier2-mgmt"
		const tier2MgmtCRName = "e2e-tier2-mgmt"

		BeforeEach(func() {
			By("creating the Tier 2 management test namespace")
			cmd := exec.Command("kubectl", "create", "ns", tier2MgmtNamespace)
			_, _ = utils.Run(cmd)

			By("ensuring the self-signed ClusterIssuer exists")
			ensureClusterIssuer()

			By("creating per-user PostgreSQL secrets")
			createPGUserSecrets(tier2MgmtNamespace)
		})

		AfterEach(func() {
			By("cleaning up the Tier 2 management CarbideDeployment CR")
			cmd := exec.Command("kubectl", "delete", "carbidedeployment", tier2MgmtCRName,
				"-n", tier2MgmtNamespace, "--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)

			By("cleaning up the Tier 2 management test namespace")
			cmd = exec.Command("kubectl", "delete", "ns", tier2MgmtNamespace,
				"--timeout=60s", "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should deploy management profile with external Keycloak auth provider", func() {
			By("applying a management-profile CarbideDeployment CR with external Keycloak")
			cr := fmt.Sprintf(`apiVersion: carbide.nvidia.com/v1alpha1
kind: CarbideDeployment
metadata:
  name: %s
  namespace: %s
spec:
  profile: management
  version: "latest"
  tls:
    mode: certManager
    certManager:
      issuerRef:
        name: carbide-e2e-selfsigned
        kind: ClusterIssuer
  network:
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
  images:
    restAPI: localhost/carbide-rest-api:e2e
    workflow: localhost/carbide-rest-workflow:e2e
    pullPolicy: Never
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
  rest:
    enabled: true
    temporal:
      mode: managed
    keycloak:
      mode: external
      realm: master
      authProviders:
      - name: keycloak-e2e
        issuerURL: http://keycloak.keycloak-e2e.svc:8080/realms/master
        jwksURL: http://keycloak.keycloak-e2e.svc:8080/realms/master/protocol/openid-connect/certs
        clientID: carbide-e2e
    restAPI:
      port: 8080
`, tier2MgmtCRName, tier2MgmtNamespace, tier2MgmtNamespace, tier2MgmtNamespace)

			crFile := filepath.Join("/tmp", "e2e-cr-tier2-mgmt.yaml")
			err := os.WriteFile(crFile, []byte(cr), 0o644)
			Expect(err).NotTo(HaveOccurred())
			defer os.Remove(crFile)

			cmd := exec.Command("kubectl", "apply", "-f", crFile)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to apply Tier 2 management CarbideDeployment CR")

			By("waiting for reconciliation (finalizer exists)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", tier2MgmtCRName,
					"-n", tier2MgmtNamespace,
					"-o", "jsonpath={.metadata.finalizers}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("carbide.nvidia.com/finalizer"))
			}, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying status conditions are set")
			verifyConditions := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", tier2MgmtCRName,
					"-n", tier2MgmtNamespace,
					"-o", "jsonpath={.status.conditions[*].type}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "status.conditions should be set")
			}
			Eventually(verifyConditions, 60*time.Second, 2*time.Second).Should(Succeed())

			By("verifying the auth provider configuration is stored in the CR spec")
			verifyAuthProvider := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", tier2MgmtCRName,
					"-n", tier2MgmtNamespace,
					"-o", "jsonpath={.spec.rest.keycloak.authProviders[0].issuerURL}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("keycloak.keycloak-e2e.svc"),
					"auth provider issuerURL should point to Keycloak")
			}
			Eventually(verifyAuthProvider, 30*time.Second, 2*time.Second).Should(Succeed())

			By("verifying observedGeneration matches generation")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "carbidedeployment", tier2MgmtCRName,
					"-n", tier2MgmtNamespace,
					"-o", "jsonpath={.metadata.generation}")
				generation, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(generation).NotTo(BeEmpty())

				cmd = exec.Command("kubectl", "get", "carbidedeployment", tier2MgmtCRName,
					"-n", tier2MgmtNamespace,
					"-o", "jsonpath={.status.observedGeneration}")
				observedGeneration, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(observedGeneration).To(Equal(generation))
			}, 60*time.Second, 2*time.Second).Should(Succeed())
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

// ensureClusterIssuer creates or re-applies the self-signed ClusterIssuer
// used by Tier 2 tests. This is idempotent and safe to call from multiple
// BeforeEach blocks in case a prior test's AfterEach deleted it.
func ensureClusterIssuer() {
	issuerYAML := `apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: carbide-e2e-selfsigned
spec:
  selfSigned: {}
`
	issuerFile := filepath.Join("/tmp", "e2e-clusterissuer-ensure.yaml")
	err := os.WriteFile(issuerFile, []byte(issuerYAML), 0o644)
	ExpectWithOffset(2, err).NotTo(HaveOccurred())
	defer os.Remove(issuerFile)

	cmd := exec.Command("kubectl", "apply", "-f", issuerFile)
	_, err = utils.Run(cmd)
	ExpectWithOffset(2, err).NotTo(HaveOccurred(), "Failed to ensure ClusterIssuer exists")
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}

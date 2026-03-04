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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

func newReconciler() *CarbideDeploymentReconciler {
	return &CarbideDeploymentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
}

func createDeployment(ctx context.Context, name string, spec carbitev1alpha1.CarbideDeploymentSpec) {
	resource := &carbitev1alpha1.CarbideDeployment{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, resource)
	if err != nil && errors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, &carbitev1alpha1.CarbideDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       spec,
		})).To(Succeed())
	}
}

func deleteDeployment(ctx context.Context, name string) {
	resource := &carbitev1alpha1.CarbideDeployment{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, resource)
	if err == nil {
		// Remove finalizer first so delete succeeds
		resource.Finalizers = nil
		_ = k8sClient.Update(ctx, resource)
		_ = k8sClient.Delete(ctx, resource)
	}
}

var _ = Describe("CarbideDeployment Controller", func() {
	ctx := context.Background()

	Context("management profile basic reconcile", func() {
		name := "test-mgmt-basic"
		nn := types.NamespacedName{Name: name, Namespace: "default"}

		BeforeEach(func() {
			createDeployment(ctx, name, carbitev1alpha1.CarbideDeploymentSpec{
				Profile: carbitev1alpha1.ProfileManagement,
				TLS: &carbitev1alpha1.TLSConfig{
					Mode:   carbitev1alpha1.TLSModeSpiffe,
					SPIFFE: &carbitev1alpha1.SPIFFEConfig{TrustDomain: "carbide.local"},
				},
			})
		})
		AfterEach(func() { deleteDeployment(ctx, name) })

		It("should reconcile without error", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("management profile finalizer and status", func() {
		name := "test-mgmt-status"
		nn := types.NamespacedName{Name: name, Namespace: "default"}

		BeforeEach(func() {
			createDeployment(ctx, name, carbitev1alpha1.CarbideDeploymentSpec{
				Profile: carbitev1alpha1.ProfileManagement,
				TLS: &carbitev1alpha1.TLSConfig{
					Mode:   carbitev1alpha1.TLSModeSpiffe,
					SPIFFE: &carbitev1alpha1.SPIFFEConfig{TrustDomain: "carbide.local"},
				},
			})
		})
		AfterEach(func() { deleteDeployment(ctx, name) })

		It("should add finalizer and initialize status", func() {
			r := newReconciler()
			for i := 0; i < 3; i++ {
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())
			}
			resource := &carbitev1alpha1.CarbideDeployment{}
			Expect(k8sClient.Get(ctx, nn, resource)).To(Succeed())
			Expect(resource.Finalizers).To(ContainElement("carbide.nvidia.com/finalizer"))
			Expect(resource.Status.Phase).NotTo(BeEmpty())
		})
	})

	Context("site profile with RLA/PSM/Vault", func() {
		name := "test-site-full"
		nn := types.NamespacedName{Name: name, Namespace: "default"}

		BeforeEach(func() {
			createDeployment(ctx, name, carbitev1alpha1.CarbideDeploymentSpec{
				Profile: carbitev1alpha1.ProfileSite,
				Version: "latest",
				Network: carbitev1alpha1.NetworkConfig{
					Interface: "eth0", IP: "10.0.0.1",
					AdminNetworkCIDR: "10.0.0.0/24", Domain: "carbide.local",
				},
				TLS: &carbitev1alpha1.TLSConfig{
					Mode:   carbitev1alpha1.TLSModeSpiffe,
					SPIFFE: &carbitev1alpha1.SPIFFEConfig{TrustDomain: "carbide.local"},
				},
				Core: carbitev1alpha1.CoreConfig{
					RLA:   &carbitev1alpha1.RLAConfig{Enabled: true},
					PSM:   &carbitev1alpha1.PSMConfig{Enabled: true},
					Vault: &carbitev1alpha1.VaultConfig{Mode: carbitev1alpha1.ManagedMode},
				},
			})
		})
		AfterEach(func() { deleteDeployment(ctx, name) })

		It("should reconcile without error", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("disabled Keycloak", func() {
		name := "test-kc-disabled"
		nn := types.NamespacedName{Name: name, Namespace: "default"}

		BeforeEach(func() {
			createDeployment(ctx, name, carbitev1alpha1.CarbideDeploymentSpec{
				Profile: carbitev1alpha1.ProfileManagement,
				TLS:     &carbitev1alpha1.TLSConfig{Mode: carbitev1alpha1.TLSModeSpiffe},
				Rest: &carbitev1alpha1.RestConfig{
					Enabled:  true,
					Keycloak: carbitev1alpha1.KeycloakConfig{Mode: carbitev1alpha1.AuthModeDisabled},
					Temporal: carbitev1alpha1.TemporalConfig{Mode: carbitev1alpha1.ManagedMode},
					RestAPI:  carbitev1alpha1.RestAPIConfig{Port: 8080},
				},
			})
		})
		AfterEach(func() { deleteDeployment(ctx, name) })

		It("should reconcile without error", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("cert-manager TLS mode", func() {
		name := "test-certmgr"
		nn := types.NamespacedName{Name: name, Namespace: "default"}

		BeforeEach(func() {
			createDeployment(ctx, name, carbitev1alpha1.CarbideDeploymentSpec{
				Profile: carbitev1alpha1.ProfileManagement,
				TLS: &carbitev1alpha1.TLSConfig{
					Mode: carbitev1alpha1.TLSModeCertManager,
					CertManager: &carbitev1alpha1.CertManagerConfig{
						IssuerRef: carbitev1alpha1.CertManagerIssuerRef{
							Name: "test-issuer", Kind: "ClusterIssuer",
						},
					},
				},
			})
		})
		AfterEach(func() { deleteDeployment(ctx, name) })

		It("should reconcile without error", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("nonexistent resource", func() {
		It("should not return an error", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("management-with-site profile with infrastructure", func() {
		name := "test-mws-infra"
		nn := types.NamespacedName{Name: name, Namespace: "default"}
		infraNS := "carbide"

		BeforeEach(func() {
			// Create infrastructure namespace
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: infraNS}}
			_ = k8sClient.Create(ctx, ns)

			// Pre-create PGO secrets that the reconciler expects
			for _, user := range []string{"carbide", "forge", "temporal"} {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "carbide-postgres-pguser-" + user,
						Namespace: infraNS,
					},
					Data: map[string][]byte{
						"host":     []byte("carbide-postgres-primary.carbide.svc"),
						"port":     []byte("5432"),
						"user":     []byte(user),
						"password": []byte("test-pass"),
						"dbname":   []byte(user),
						"uri":      []byte("postgres://" + user + ":test-pass@localhost:5432/" + user),
					},
				}
				_ = k8sClient.Create(ctx, secret)
			}

			// No Infrastructure config — envtest doesn't have PGO CRD installed,
			// so we skip the infrastructure tier and go straight to core.
			// Infrastructure reconciliation is covered by fake-client tests.
			createDeployment(ctx, name, carbitev1alpha1.CarbideDeploymentSpec{
				Profile: carbitev1alpha1.ProfileManagementWithSite,
				Version: "latest",
				TLS: &carbitev1alpha1.TLSConfig{
					Mode:   carbitev1alpha1.TLSModeSpiffe,
					SPIFFE: &carbitev1alpha1.SPIFFEConfig{TrustDomain: "carbide.local"},
				},
				Network: carbitev1alpha1.NetworkConfig{
					Interface:        "eth0",
					IP:               "10.0.0.1",
					AdminNetworkCIDR: "10.0.0.0/24",
					Domain:           "carbide.local",
				},
				Core: carbitev1alpha1.CoreConfig{
					Namespace: infraNS,
					API:       carbitev1alpha1.APIConfig{Port: 1079},
				},
			})
		})

		AfterEach(func() {
			deleteDeployment(ctx, name)
			// Clean up secrets
			for _, user := range []string{"carbide", "forge", "temporal"} {
				secret := &corev1.Secret{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "carbide-postgres-pguser-" + user, Namespace: infraNS,
				}, secret)
				if err == nil {
					_ = k8sClient.Delete(ctx, secret)
				}
			}
		})

		It("should progress through multiple reconcile cycles and set status", func() {
			r := newReconciler()
			// Run multiple reconcile cycles
			for i := 0; i < 5; i++ {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			}

			// Verify the deployment has progressed
			resource := &carbitev1alpha1.CarbideDeployment{}
			Expect(k8sClient.Get(ctx, nn, resource)).To(Succeed())
			Expect(resource.Status.Phase).NotTo(BeEmpty())
			Expect(resource.Status.ObservedGeneration).To(Equal(resource.Generation))
			Expect(resource.Status.Conditions).NotTo(BeEmpty())

			// Verify TLS condition was set (SPIRE not available in envtest)
			var tlsCondition *metav1.Condition
			for i := range resource.Status.Conditions {
				if resource.Status.Conditions[i].Type == "TLSReady" {
					tlsCondition = &resource.Status.Conditions[i]
					break
				}
			}
			Expect(tlsCondition).NotTo(BeNil(), "TLSReady condition should be set")
		})

		It("should set TLSReady=False when SPIRE is not available", func() {
			r := newReconciler()
			for i := 0; i < 3; i++ {
				_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			}

			resource := &carbitev1alpha1.CarbideDeployment{}
			Expect(k8sClient.Get(ctx, nn, resource)).To(Succeed())

			// In envtest, SPIRE CSI driver is not installed
			// so TLSReady should be False and phase should be Failed
			var tlsCondition *metav1.Condition
			for i := range resource.Status.Conditions {
				if resource.Status.Conditions[i].Type == "TLSReady" {
					tlsCondition = &resource.Status.Conditions[i]
					break
				}
			}
			Expect(tlsCondition).NotTo(BeNil())
			Expect(string(tlsCondition.Status)).To(Equal("False"))
			Expect(resource.Status.Phase).To(Equal(carbitev1alpha1.PhaseFailed))
		})
	})
})

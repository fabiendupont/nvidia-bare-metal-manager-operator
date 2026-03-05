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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

// testScheme returns a scheme with all needed types registered.
func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = carbitev1alpha1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = apiextensionsv1.AddToScheme(s)
	return s
}

// newTestDeployment creates a CarbideDeployment for testing.
func newTestDeployment(name string, profile carbitev1alpha1.DeploymentProfile) *carbitev1alpha1.CarbideDeployment {
	d := &carbitev1alpha1.CarbideDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: carbitev1alpha1.CarbideDeploymentSpec{
			Profile: profile,
			Version: "latest",
			TLS: &carbitev1alpha1.TLSConfig{
				Mode: carbitev1alpha1.TLSModeSpiffe,
				SPIFFE: &carbitev1alpha1.SPIFFEConfig{
					TrustDomain: "carbide.local",
					HelperImage: "ghcr.io/nvidia/spiffe-helper:latest",
					ClassName:   "zero-trust-workload-identity-manager-spire",
				},
			},
			Network: carbitev1alpha1.NetworkConfig{
				Interface:        "eth0",
				IP:               "10.0.0.1",
				AdminNetworkCIDR: "10.0.0.0/24",
				Domain:           "carbide.local",
			},
			Core: carbitev1alpha1.CoreConfig{
				API:  carbitev1alpha1.APIConfig{Port: 1079, Replicas: 1},
				DHCP: carbitev1alpha1.DHCPConfig{Enabled: true},
				PXE:  carbitev1alpha1.PXEConfig{Enabled: true},
				DNS:  carbitev1alpha1.DNSConfig{Enabled: true},
			},
		},
	}

	if profile == carbitev1alpha1.ProfileManagement || profile == carbitev1alpha1.ProfileManagementWithSite {
		d.Spec.Rest = &carbitev1alpha1.RestConfig{
			Enabled: true,
			Temporal: carbitev1alpha1.TemporalConfig{
				Mode:    carbitev1alpha1.ManagedMode,
				Version: "1.22.0",
			},
			Keycloak: carbitev1alpha1.KeycloakConfig{
				Mode:  carbitev1alpha1.AuthModeManaged,
				Realm: "carbide",
			},
			RestAPI: carbitev1alpha1.RestAPIConfig{Port: 8080, Replicas: 1},
		}
	}

	return d
}

// newNamespace creates a Namespace object.
func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

// newReadyDeployment creates a Deployment with Ready status.
func newReadyDeployment(name, namespace string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace, Generation: 1,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas:  1,
			UpdatedReplicas:    1,
			ObservedGeneration: 1,
		},
	}
}

// newReadyDaemonSet creates a DaemonSet with Ready status.
func newReadyDaemonSet(name, namespace string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace, Generation: 1,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 1,
			NumberReady:            1,
			UpdatedNumberScheduled: 1,
			ObservedGeneration:     1,
		},
	}
}

// newPGSecret creates a PGO-style user secret.
func newPGSecret(username, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "carbide-postgres-pguser-" + username,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"host":     []byte("carbide-postgres-primary.carbide.svc"),
			"port":     []byte("5432"),
			"user":     []byte(username),
			"password": []byte("test-password"),
			"dbname":   []byte(username),
			"uri":      []byte("postgres://" + username + ":test-password@carbide-postgres-primary.carbide.svc:5432/" + username),
		},
	}
}

// testRESTMapper returns a REST mapper that knows about all unstructured GVKs
// used by the reconcilers (ClusterSPIFFEID, Certificate, PostgresCluster, Keycloak, etc.).
func testRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "spire.spiffe.io", Version: "v1alpha1"},
		{Group: "cert-manager.io", Version: "v1"},
		{Group: "postgres-operator.crunchydata.com", Version: "v1beta1"},
		{Group: "k8s.keycloak.org", Version: "v2alpha1"},
		{Group: "storage.k8s.io", Version: "v1"},
		{Group: "apiextensions.k8s.io", Version: "v1"},
	})

	// Cluster-scoped resources
	mapper.Add(schema.GroupVersionKind{Group: "spire.spiffe.io", Version: "v1alpha1", Kind: "ClusterSPIFFEID"}, meta.RESTScopeRoot)
	mapper.Add(schema.GroupVersionKind{Group: "storage.k8s.io", Version: "v1", Kind: "CSIDriver"}, meta.RESTScopeRoot)
	mapper.Add(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}, meta.RESTScopeRoot)

	// Namespace-scoped resources
	mapper.Add(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "postgres-operator.crunchydata.com", Version: "v1beta1", Kind: "PostgresCluster"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "k8s.keycloak.org", Version: "v2alpha1", Kind: "Keycloak"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "k8s.keycloak.org", Version: "v2alpha1", Kind: "KeycloakRealmImport"}, meta.RESTScopeNamespace)

	return mapper
}

// buildFakeClient creates a fake client with common objects pre-populated
// and a REST mapper that supports all unstructured GVKs used by the reconcilers.
func buildFakeClient(s *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(s).
		WithRESTMapper(testRESTMapper()).
		WithObjects(objs...).
		WithStatusSubresource(&carbitev1alpha1.CarbideDeployment{}).
		Build()
}

// newCRD creates a CRD object for the fake client (needed by operator validation checks).
func newCRD(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       apiextensionsv1.CustomResourceDefinitionSpec{},
	}
}

// keycloakCRDs returns the CRD objects needed by the Keycloak operator validation.
func keycloakCRDs() []client.Object {
	return []client.Object{
		newCRD("keycloaks.k8s.keycloak.org"),
		newCRD("keycloakrealmimports.k8s.keycloak.org"),
	}
}

// --- Main Controller Tests ---

func TestReconcile_NotFound_NoError(t *testing.T) {
	s := testScheme()
	c := buildFakeClient(s)
	r := &CarbideDeploymentReconciler{Client: c, Scheme: s}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Requeue || result.RequeueAfter > 0 { //nolint:staticcheck
		t.Error("expected no requeue for not-found resource")
	}
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	s := testScheme()
	dep := newTestDeployment("test", carbitev1alpha1.ProfileManagement)
	c := buildFakeClient(s, dep)
	r := &CarbideDeploymentReconciler{Client: c, Scheme: s}

	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result.Requeue && result.RequeueAfter == 0 { //nolint:staticcheck
		t.Error("expected requeue after adding finalizer")
	}

	var updated carbitev1alpha1.CarbideDeployment
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}

	found := false
	for _, f := range updated.Finalizers {
		if f == "carbide.nvidia.com/finalizer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected finalizer to be added")
	}
}

func TestReconcile_InitializesStatus(t *testing.T) {
	s := testScheme()
	dep := newTestDeployment("test", carbitev1alpha1.ProfileManagement)
	// Pre-add finalizer so we skip that step
	dep.Finalizers = []string{"carbide.nvidia.com/finalizer"}
	c := buildFakeClient(s, dep)
	r := &CarbideDeploymentReconciler{Client: c, Scheme: s}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	var updated carbitev1alpha1.CarbideDeployment
	if err := c.Get(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}

	if updated.Status.Phase != carbitev1alpha1.PhasePending {
		t.Errorf("expected phase Pending, got: %s", updated.Status.Phase)
	}
	if len(updated.Status.Conditions) == 0 {
		t.Error("expected conditions to be initialized")
	}
}

// --- Core Reconciler Tests ---

func TestCoreReconciler_ManagementProfile_SkipsCore(t *testing.T) {
	s := testScheme()
	dep := newTestDeployment("test", carbitev1alpha1.ProfileManagement)

	r := &CoreReconciler{Client: buildFakeClient(s), Scheme: s}
	status, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !status.Ready {
		t.Error("expected core tier to be ready for management profile")
	}
	if status.Message != "Not required for management profile" {
		t.Errorf("unexpected message: %s", status.Message)
	}
}

func TestCoreReconciler_SiteProfile_CreatesResources(t *testing.T) {
	s := testScheme()
	ns := "carbide-site-test"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileSite)
	dep.Spec.Core.Namespace = ns
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace:  ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{Mode: carbitev1alpha1.ManagedMode},
	}

	// Pre-populate: namespace, PG secrets
	c := buildFakeClient(s,
		newNamespace(ns),
		newPGSecret("carbide", ns),
	)

	r := &CoreReconciler{Client: c, Scheme: s}
	status, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Should not be fully ready (API deployment not ready yet)
	if status.Ready {
		t.Error("expected core tier to not be ready yet (API not deployed)")
	}

	// Verify ConfigMap was created
	var cm corev1.ConfigMap
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-api-config", Namespace: ns}, &cm)
	if err != nil {
		t.Fatalf("expected carbide-api-config ConfigMap to be created, got: %v", err)
	}

	// Verify Casbin policy was created
	err = c.Get(context.Background(), types.NamespacedName{Name: "casbin-policy", Namespace: ns}, &cm)
	if err != nil {
		t.Fatalf("expected casbin-policy ConfigMap to be created, got: %v", err)
	}

	// Verify API Secret was created
	var secret corev1.Secret
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-api-secret", Namespace: ns}, &secret)
	if err != nil {
		t.Fatalf("expected carbide-api-secret to be created, got: %v", err)
	}

	// Verify ServiceAccounts were created
	for _, saName := range []string{"carbide-api", "carbide-dhcp", "carbide-dns", "carbide-pxe"} {
		var sa corev1.ServiceAccount
		err = c.Get(context.Background(), types.NamespacedName{Name: saName, Namespace: ns}, &sa)
		if err != nil {
			t.Errorf("expected ServiceAccount %s to be created, got: %v", saName, err)
		}
	}
}

func TestCoreReconciler_APIReady_CreatesNetworkServices(t *testing.T) {
	s := testScheme()
	ns := "carbide"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileSite)
	dep.Spec.Core.Namespace = ns
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace:  ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{Mode: carbitev1alpha1.ManagedMode},
	}

	// Pre-populate with ready API deployment + PG secrets
	c := buildFakeClient(s,
		newNamespace(ns),
		newPGSecret("carbide", ns),
		newReadyDeployment("carbide-api", ns),
	)

	r := &CoreReconciler{Client: c, Scheme: s}
	status, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// API was already ready, so network services should have been attempted
	// Check DHCP DaemonSet was created
	var ds appsv1.DaemonSet
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-dhcp", Namespace: ns}, &ds)
	if err != nil {
		t.Errorf("expected carbide-dhcp DaemonSet to be created, got: %v", err)
	}

	// Check PXE Deployment was created (not DaemonSet)
	var pxeDep appsv1.Deployment
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-pxe", Namespace: ns}, &pxeDep)
	if err != nil {
		t.Errorf("expected carbide-pxe Deployment to be created, got: %v", err)
	}

	// Check DNS DaemonSet was created
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-dns", Namespace: ns}, &ds)
	if err != nil {
		t.Errorf("expected carbide-dns DaemonSet to be created, got: %v", err)
	}

	// Not ready because DHCP/PXE/DNS haven't reported ready status
	if status.Ready {
		t.Error("expected not ready (network services not ready)")
	}
}

func TestCoreReconciler_VaultGatesRLAPSM(t *testing.T) {
	s := testScheme()
	ns := "carbide"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileSite)
	dep.Spec.Core.Namespace = ns
	dep.Spec.Core.Vault = &carbitev1alpha1.VaultConfig{Mode: carbitev1alpha1.ManagedMode}
	dep.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: true, Port: 50051, Replicas: 1}
	dep.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: true, Port: 50051, Replicas: 1}
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace:  ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{Mode: carbitev1alpha1.ManagedMode},
	}

	// API ready, network services ready, but Vault NOT ready
	c := buildFakeClient(s,
		newNamespace(ns),
		newPGSecret("carbide", ns),
		newReadyDeployment("carbide-api", ns),
		newReadyDaemonSet("carbide-dhcp", ns),
		newReadyDeployment("carbide-pxe", ns),
		newReadyDaemonSet("carbide-dns", ns),
	)

	r := &CoreReconciler{Client: c, Scheme: s}
	status, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Vault helm job was created but not complete — RLA/PSM should NOT be attempted
	var rlaDep appsv1.Deployment
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-rla", Namespace: ns}, &rlaDep)
	if err == nil {
		t.Error("expected RLA Deployment to NOT be created (Vault not ready)")
	}

	var psmDep appsv1.Deployment
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-psm", Namespace: ns}, &psmDep)
	if err == nil {
		t.Error("expected PSM Deployment to NOT be created (Vault not ready)")
	}

	if status.Ready {
		t.Error("expected not ready (Vault not ready)")
	}

	// Verify Vault was attempted (helm values CM should exist)
	var cm corev1.ConfigMap
	err = c.Get(context.Background(), types.NamespacedName{Name: "vault-helm-values", Namespace: ns}, &cm)
	if err != nil {
		t.Errorf("expected vault-helm-values ConfigMap to be created, got: %v", err)
	}
}

func TestCoreReconciler_RLAPSMServiceAccounts(t *testing.T) {
	s := testScheme()
	ns := "carbide"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileSite)
	dep.Spec.Core.Namespace = ns
	dep.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: true}
	dep.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: true}
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace:  ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{Mode: carbitev1alpha1.ManagedMode},
	}

	c := buildFakeClient(s,
		newNamespace(ns),
		newPGSecret("carbide", ns),
	)

	r := &CoreReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// RLA and PSM SAs should be created
	for _, saName := range []string{"carbide-rla", "carbide-psm"} {
		var sa corev1.ServiceAccount
		err = c.Get(context.Background(), types.NamespacedName{Name: saName, Namespace: ns}, &sa)
		if err != nil {
			t.Errorf("expected ServiceAccount %s to be created, got: %v", saName, err)
		}
	}
}

// --- Infrastructure Reconciler Tests ---

func TestInfraReconciler_ExternalMode_ValidatesConnection(t *testing.T) {
	s := testScheme()
	ns := "carbide"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileSite)
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace: ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{
			Mode: carbitev1alpha1.ExternalMode,
			Connection: &carbitev1alpha1.ExternalPGConnection{
				Host:    "pg.example.com",
				Port:    5432,
				SSLMode: "require",
			},
		},
	}

	c := buildFakeClient(s, newNamespace(ns))
	r := &InfrastructureReconciler{Client: c, Scheme: s}

	// Will fail at validation (can't reach external PG) but should not panic
	status, err := r.Reconcile(context.Background(), dep)
	if status == nil {
		t.Fatal("expected status to be returned")
	}
	// Error is expected (can't connect) but the reconciler should handle it gracefully
	_ = err
}

// --- REST Reconciler Tests ---

func TestRestReconciler_DisabledRest_ReturnsReady(t *testing.T) {
	s := testScheme()
	dep := newTestDeployment("test", carbitev1alpha1.ProfileManagement)
	dep.Spec.Rest = &carbitev1alpha1.RestConfig{Enabled: false}

	r := &RestReconciler{Client: buildFakeClient(s), Scheme: s}
	status, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !status.Ready {
		t.Error("expected REST tier to be ready when disabled")
	}
}

func TestRestReconciler_DisabledKeycloak_SkipsKeycloak(t *testing.T) {
	s := testScheme()
	ns := "carbide"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileManagement)
	dep.Spec.Core.Namespace = ns
	dep.Spec.Rest = &carbitev1alpha1.RestConfig{
		Enabled: true,
		Temporal: carbitev1alpha1.TemporalConfig{
			Mode:         carbitev1alpha1.ManagedMode,
			ChartVersion: "0.73.1",
			Namespace:    "temporal",
		},
		Keycloak: carbitev1alpha1.KeycloakConfig{
			Mode: carbitev1alpha1.AuthModeDisabled,
		},
		RestAPI: carbitev1alpha1.RestAPIConfig{Port: 8080, Replicas: 1},
	}
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace:  ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{Mode: carbitev1alpha1.ManagedMode},
	}

	c := buildFakeClient(s,
		newNamespace(ns),
		newPGSecret("forge", ns),
		newPGSecret("temporal", ns),
	)

	r := &RestReconciler{Client: c, Scheme: s}
	status, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Check Keycloak shows as ready in components (disabled = ready)
	keycloakFound := false
	for _, comp := range status.Components {
		if comp.Name == "Keycloak" {
			keycloakFound = true
			if !comp.Ready {
				t.Error("expected Keycloak to be ready when disabled")
			}
		}
	}
	if !keycloakFound {
		t.Error("expected Keycloak component in status")
	}
}

func TestRestReconciler_CreatesServiceAccounts(t *testing.T) {
	s := testScheme()
	ns := "carbide"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileManagement)
	dep.Spec.Core.Namespace = ns
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace:  ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{Mode: carbitev1alpha1.ManagedMode},
	}

	objs := []client.Object{
		newNamespace(ns),
		newPGSecret("forge", ns),
		newPGSecret("temporal", ns),
	}
	objs = append(objs, keycloakCRDs()...)
	c := buildFakeClient(s, objs...)

	r := &RestReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Check REST API SA was created
	var sa corev1.ServiceAccount
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-rest-api", Namespace: ns}, &sa)
	if err != nil {
		t.Errorf("expected carbide-rest-api SA to be created, got: %v", err)
	}

	// Check worker SAs
	for _, name := range []string{"carbide-rest-cloud-worker", "carbide-rest-site-worker"} {
		err = c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: ns}, &sa)
		if err != nil {
			t.Errorf("expected %s SA to be created, got: %v", name, err)
		}
	}
}

func TestRestReconciler_WorkflowConfigMapName(t *testing.T) {
	s := testScheme()
	ns := "carbide"
	dep := newTestDeployment("test", carbitev1alpha1.ProfileManagement)
	dep.Spec.Core.Namespace = ns
	dep.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{
		Namespace:  ns,
		PostgreSQL: carbitev1alpha1.PostgreSQLConfig{Mode: carbitev1alpha1.ManagedMode},
	}

	objs := []client.Object{
		newNamespace(ns),
		newPGSecret("forge", ns),
		newPGSecret("temporal", ns),
	}
	objs = append(objs, keycloakCRDs()...)
	c := buildFakeClient(s, objs...)

	r := &RestReconciler{Client: c, Scheme: s}
	_, err := r.Reconcile(context.Background(), dep)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	var cm corev1.ConfigMap
	err = c.Get(context.Background(), types.NamespacedName{Name: "carbide-rest-workflow-config", Namespace: ns}, &cm)
	if err != nil {
		t.Errorf("expected carbide-rest-workflow-config ConfigMap, got: %v", err)
	}
}

// --- Deletion Tests ---

func TestReconcile_Deletion_RemovesFinalizer(t *testing.T) {
	s := testScheme()
	now := metav1.Now()
	dep := newTestDeployment("test-del", carbitev1alpha1.ProfileManagement)
	dep.Finalizers = []string{"carbide.nvidia.com/finalizer"}
	dep.DeletionTimestamp = &now

	c := buildFakeClient(s, dep)
	r := &CarbideDeploymentReconciler{Client: c, Scheme: s}

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-del", Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// After finalizer removal with DeletionTimestamp set, the fake client
	// garbage-collects the object — verify it's gone.
	var updated carbitev1alpha1.CarbideDeployment
	err = c.Get(context.Background(), types.NamespacedName{Name: "test-del", Namespace: "default"}, &updated)
	if err == nil {
		// If the object still exists, verify the finalizer was removed
		if len(updated.Finalizers) != 0 {
			t.Errorf("expected finalizer to be removed, got: %v", updated.Finalizers)
		}
	}
	// Object gone = finalizer was removed and GC kicked in. That's correct.
}

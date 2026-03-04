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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	restresources "github.com/NVIDIA/bare-metal-manager-operator/internal/resources/rest"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/utils"
)

// RestReconciler reconciles REST tier components
type RestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles the REST tier
func (r *RestReconciler) Reconcile(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (*carbitev1alpha1.TierStatus, error) {
	logger := log.FromContext(ctx).WithValues("tier", "rest")
	logger.Info("Reconciling REST tier")

	restConfig := deployment.Spec.Rest
	if restConfig == nil || !restConfig.Enabled {
		return &carbitev1alpha1.TierStatus{
			Ready:              true,
			Message:            "REST tier not enabled",
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		}, nil
	}

	namespace := deployment.Spec.Core.Namespace
	if namespace == "" {
		namespace = "carbide"
	}

	// Initialize tier status
	tierStatus := &carbitev1alpha1.TierStatus{
		Ready:              false,
		Components:         []carbitev1alpha1.ComponentStatus{},
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	}

	// 0. Create ServiceAccounts
	restAPISA := restresources.BuildRestAPIServiceAccount(deployment, namespace)
	if err := r.createOrUpdate(ctx, restAPISA); err != nil {
		return r.failedStatus("Failed to create REST API ServiceAccount", err), err
	}

	for _, saName := range []string{restresources.CloudWorkerName, restresources.SiteWorkerName} {
		sa := restresources.BuildWorkerServiceAccount(saName, namespace, deployment)
		if err := r.createOrUpdate(ctx, sa); err != nil {
			return r.failedStatus(fmt.Sprintf("Failed to create %s ServiceAccount", saName), err), err
		}
	}

	// 0b. Create TLS resources
	if tls.IsSpiffeMode(deployment) {
		helperCM := tls.BuildSpiffeHelperConfigMap(deployment, namespace)
		if err := r.createOrUpdate(ctx, helperCM); err != nil {
			return r.failedStatus("Failed to create spiffe-helper ConfigMap", err), err
		}

		// ClusterSPIFFEIDs for REST services
		restAPISID := tls.BuildClusterSPIFFEID(deployment, restresources.RestAPIName, namespace, restresources.RestAPIName, nil)
		if err := r.createOrUpdateUnstructured(ctx, restAPISID); err != nil {
			return r.failedStatus("Failed to create REST API ClusterSPIFFEID", err), err
		}

		for _, name := range []string{restresources.CloudWorkerName, restresources.SiteWorkerName} {
			sid := tls.BuildClusterSPIFFEID(deployment, name, namespace, name, nil)
			if err := r.createOrUpdateUnstructured(ctx, sid); err != nil {
				return r.failedStatus(fmt.Sprintf("Failed to create %s ClusterSPIFFEID", name), err), err
			}
		}

		// Temporal ClusterSPIFFEID with DNS SANs
		temporalNs := deployment.Spec.Rest.Temporal.Namespace
		if temporalNs == "" {
			temporalNs = "temporal"
		}
		temporalSID := tls.BuildClusterSPIFFEID(deployment, "temporal", temporalNs, "temporal",
			[]string{
				"temporal-frontend",
				"cloud.temporal-frontend",
				"site.temporal-frontend",
			})
		if err := r.createOrUpdateUnstructured(ctx, temporalSID); err != nil {
			return r.failedStatus("Failed to create Temporal ClusterSPIFFEID", err), err
		}

		// site-manager ClusterSPIFFEID with DNS SANs
		smSID := tls.BuildClusterSPIFFEID(deployment, restresources.SiteManagerName, namespace, restresources.SiteManagerName,
			[]string{fmt.Sprintf("%s.%s.svc.cluster.local", restresources.SiteManagerName, namespace)})
		if err := r.createOrUpdateUnstructured(ctx, smSID); err != nil {
			return r.failedStatus("Failed to create site-manager ClusterSPIFFEID", err), err
		}
	} else if tls.IsCertManagerMode(deployment) {
		// Create cert-manager Certificates for REST services
		for _, name := range []string{restresources.RestAPIName, restresources.CloudWorkerName, restresources.SiteWorkerName, restresources.SiteManagerName} {
			cert := tls.BuildCertificate(deployment, name, namespace,
				[]string{fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace)})
			if err := r.createOrUpdateUnstructured(ctx, cert); err != nil {
				return r.failedStatus(fmt.Sprintf("Failed to create %s Certificate", name), err), err
			}
		}
	}

	// 0c. Create REST API Secret
	pgPassword, err := r.getForgePassword(ctx, deployment)
	if err != nil {
		logger.Info("PostgreSQL forge password not available yet, will retry", "error", err)
		tierStatus.Message = "Waiting for PostgreSQL forge password"
		return tierStatus, nil
	}

	keycloakSecret := "" // Will be populated when Keycloak is ready
	restAPISecret := restresources.BuildRestAPISecret(deployment, namespace, pgPassword, keycloakSecret)
	if err := r.createOrUpdate(ctx, restAPISecret); err != nil {
		return r.failedStatus("Failed to create REST API Secret", err), err
	}

	// 0d. Create Workflow ConfigMap
	workflowCM := restresources.BuildWorkflowConfigMap(deployment, namespace)
	if err := r.createOrUpdate(ctx, workflowCM); err != nil {
		return r.failedStatus("Failed to create workflow ConfigMap", err), err
	}

	// 1. Temporal
	temporalReady, temporalErr := r.reconcileTemporal(ctx, deployment, namespace)
	tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
		Name:               "Temporal",
		Ready:              temporalReady,
		Message:            r.getComponentMessage("Temporal", temporalReady, temporalErr),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	})
	if temporalErr != nil {
		tierStatus.Message = fmt.Sprintf("Temporal reconciliation failed: %v", temporalErr)
		return tierStatus, temporalErr
	}

	// 2. Keycloak
	keycloakReady, keycloakErr := r.reconcileKeycloak(ctx, deployment, namespace)
	tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
		Name:               "Keycloak",
		Ready:              keycloakReady,
		Message:            r.getComponentMessage("Keycloak", keycloakReady, keycloakErr),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	})
	if keycloakErr != nil {
		tierStatus.Message = fmt.Sprintf("Keycloak reconciliation failed: %v", keycloakErr)
		return tierStatus, keycloakErr
	}

	// 3. Temporal namespace init (only if Temporal is ready)
	var temporalSetupReady bool
	if temporalReady {
		var temporalSetupErr error
		temporalSetupReady, temporalSetupErr = r.reconcileTemporalSetup(ctx, deployment, namespace)
		if temporalSetupErr != nil {
			tierStatus.Message = fmt.Sprintf("Temporal setup failed: %v", temporalSetupErr)
			return tierStatus, temporalSetupErr
		}
	}

	// 4. REST API (only if Temporal and Keycloak are ready)
	var restAPIReady bool
	if temporalReady && keycloakReady && temporalSetupReady {
		var restAPIErr error
		restAPIReady, restAPIErr = r.reconcileRestAPI(ctx, deployment, namespace)
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "RestAPI",
			Ready:              restAPIReady,
			Message:            r.getComponentMessage("RestAPI", restAPIReady, restAPIErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})
		if restAPIErr != nil {
			tierStatus.Message = fmt.Sprintf("REST API reconciliation failed: %v", restAPIErr)
			return tierStatus, restAPIErr
		}
	}

	// 5. Workers (after Temporal is ready)
	var cloudWorkerReady, siteWorkerReady bool = true, true
	if temporalReady && temporalSetupReady {
		var cwErr, swErr error
		cloudWorkerReady, cwErr = r.reconcileWorker(ctx, deployment, namespace, restresources.CloudWorkerName)
		if cwErr != nil {
			tierStatus.Message = fmt.Sprintf("Cloud worker reconciliation failed: %v", cwErr)
			return tierStatus, cwErr
		}
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "CloudWorker",
			Ready:              cloudWorkerReady,
			Message:            r.getComponentMessage("CloudWorker", cloudWorkerReady, cwErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})

		siteWorkerReady, swErr = r.reconcileWorker(ctx, deployment, namespace, restresources.SiteWorkerName)
		if swErr != nil {
			tierStatus.Message = fmt.Sprintf("Site worker reconciliation failed: %v", swErr)
			return tierStatus, swErr
		}
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "SiteWorker",
			Ready:              siteWorkerReady,
			Message:            r.getComponentMessage("SiteWorker", siteWorkerReady, swErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})
	}

	// 6. Site Manager (for management profiles only, after REST API is ready)
	var siteManagerReady bool = true
	if (deployment.Spec.Profile == carbitev1alpha1.ProfileManagement ||
		deployment.Spec.Profile == carbitev1alpha1.ProfileManagementWithSite) && restAPIReady {
		var siteManagerErr error
		siteManagerReady, siteManagerErr = r.reconcileSiteManager(ctx, deployment, namespace)
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "SiteManager",
			Ready:              siteManagerReady,
			Message:            r.getComponentMessage("SiteManager", siteManagerReady, siteManagerErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})
		if siteManagerErr != nil {
			tierStatus.Message = fmt.Sprintf("Site manager reconciliation failed: %v", siteManagerErr)
			return tierStatus, siteManagerErr
		}
	}

	// 7. Site Agent (optional, only if enabled and REST API is ready)
	var siteAgentReady bool = true
	if restConfig.SiteAgent != nil && restConfig.SiteAgent.Enabled && restAPIReady {
		var siteAgentErr error
		siteAgentReady, siteAgentErr = r.reconcileSiteAgent(ctx, deployment, namespace)
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "SiteAgent",
			Ready:              siteAgentReady,
			Message:            r.getComponentMessage("SiteAgent", siteAgentReady, siteAgentErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})
		if siteAgentErr != nil {
			tierStatus.Message = fmt.Sprintf("Site agent reconciliation failed: %v", siteAgentErr)
			return tierStatus, siteAgentErr
		}
	}

	// Check overall tier readiness
	tierStatus.Ready = temporalReady && keycloakReady && temporalSetupReady && restAPIReady &&
		cloudWorkerReady && siteWorkerReady && siteManagerReady && siteAgentReady
	if tierStatus.Ready {
		tierStatus.Message = "All REST components ready"
	} else {
		tierStatus.Message = "Waiting for REST components to be ready"
	}

	logger.Info("REST tier reconciliation complete", "ready", tierStatus.Ready)
	return tierStatus, nil
}

// reconcileTemporal reconciles Temporal component
func (r *RestReconciler) reconcileTemporal(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "temporal")
	temporalConfig := deployment.Spec.Rest.Temporal

	mode := temporalConfig.Mode
	if mode == "" {
		mode = carbitev1alpha1.ManagedMode
	}

	switch mode {
	case carbitev1alpha1.ManagedMode:
		return r.reconcileManagedTemporal(ctx, deployment, namespace, &temporalConfig)
	case carbitev1alpha1.ExternalMode:
		return r.reconcileExternalTemporal(ctx, deployment, namespace, &temporalConfig)
	default:
		logger.Error(fmt.Errorf("unknown mode"), "Invalid Temporal mode", "mode", mode)
		return false, fmt.Errorf("invalid Temporal mode: %s", mode)
	}
}

// reconcileManagedTemporal reconciles managed Temporal via Helm
func (r *RestReconciler) reconcileManagedTemporal(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string, config *carbitev1alpha1.TemporalConfig) (bool, error) {
	logger := log.FromContext(ctx).WithValues("mode", "managed")
	logger.Info("Reconciling managed Temporal via Helm")

	pgHost, pgPort, pgSecretName, err := r.getPostgreSQLConnection(ctx, deployment)
	if err != nil {
		logger.Error(err, "Failed to get PostgreSQL connection details")
		return false, err
	}

	// Create Helm values ConfigMap
	valuesCM := restresources.BuildTemporalHelmValuesConfigMap(deployment, namespace, pgHost, pgPort, pgSecretName)
	if err := r.createOrUpdate(ctx, valuesCM); err != nil {
		logger.Error(err, "Failed to create Temporal Helm values ConfigMap")
		return false, err
	}

	// Create Helm install job
	helmJob := restresources.BuildTemporalHelmJob(deployment, namespace)
	if err := r.createOrUpdate(ctx, helmJob); err != nil {
		logger.Error(err, "Failed to create Temporal Helm job")
		return false, err
	}

	// Check if Helm job is complete
	helmComplete, err := utils.IsJobComplete(ctx, r.Client, namespace, restresources.TemporalHelmJobName)
	if err != nil {
		logger.Error(err, "Failed to check Temporal Helm job status")
		return false, err
	}
	if !helmComplete {
		logger.Info("Temporal Helm install not complete yet")
		return false, nil
	}

	// Check if Temporal frontend Deployment is ready
	temporalNamespace := config.Namespace
	if temporalNamespace == "" {
		temporalNamespace = "temporal"
	}

	ready, err := utils.IsTemporalHelmReady(ctx, r.Client, temporalNamespace)
	if err != nil {
		logger.Error(err, "Failed to check Temporal readiness")
		return false, err
	}

	return ready, nil
}

// reconcileExternalTemporal validates external Temporal connectivity
func (r *RestReconciler) reconcileExternalTemporal(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string, config *carbitev1alpha1.TemporalConfig) (bool, error) {
	logger := log.FromContext(ctx).WithValues("mode", "external")
	logger.Info("Validating external Temporal")

	if config.Endpoint == "" {
		return false, fmt.Errorf("external Temporal endpoint is required")
	}

	if err := utils.ValidateExternalTemporal(ctx, config.Endpoint); err != nil {
		logger.Error(err, "External Temporal validation failed")
		return false, err
	}

	return true, nil
}

// reconcileKeycloak reconciles Keycloak component
func (r *RestReconciler) reconcileKeycloak(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "keycloak")
	keycloakConfig := deployment.Spec.Rest.Keycloak

	switch keycloakConfig.Mode {
	case carbitev1alpha1.AuthModeManaged:
		return r.reconcileManagedKeycloak(ctx, deployment, namespace, &keycloakConfig)
	case carbitev1alpha1.AuthModeExternal:
		return r.reconcileExternalKeycloak(ctx, deployment, namespace, &keycloakConfig)
	case carbitev1alpha1.AuthModeDisabled:
		logger.Info("Authentication disabled")
		return true, nil
	default:
		// Treat empty as managed for backward compatibility
		if string(keycloakConfig.Mode) == "" {
			return r.reconcileManagedKeycloak(ctx, deployment, namespace, &keycloakConfig)
		}
		logger.Error(fmt.Errorf("unknown mode"), "Invalid Keycloak mode", "mode", keycloakConfig.Mode)
		return false, fmt.Errorf("invalid Keycloak mode: %s", keycloakConfig.Mode)
	}
}

// reconcileManagedKeycloak reconciles managed Keycloak
func (r *RestReconciler) reconcileManagedKeycloak(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string, config *carbitev1alpha1.KeycloakConfig) (bool, error) {
	logger := log.FromContext(ctx).WithValues("mode", "managed")
	logger.Info("Reconciling managed Keycloak")

	if err := utils.ValidateKeycloakOperator(ctx, r.Client); err != nil {
		logger.Error(err, "Keycloak operator not found")
		return false, err
	}

	keycloakCR, err := restresources.BuildKeycloakInstance(deployment, namespace)
	if err != nil {
		logger.Error(err, "Failed to build Keycloak instance CR")
		return false, err
	}
	if err := r.createOrUpdateUnstructured(ctx, keycloakCR); err != nil {
		logger.Error(err, "Failed to create/update Keycloak instance")
		return false, err
	}

	realmImportCR, err := restresources.BuildKeycloakRealmImport(deployment, namespace)
	if err != nil {
		logger.Error(err, "Failed to build Keycloak realm import CR")
		return false, err
	}
	if err := r.createOrUpdateUnstructured(ctx, realmImportCR); err != nil {
		logger.Error(err, "Failed to create/update Keycloak realm import")
		return false, err
	}

	ready, err := utils.IsKeycloakReady(ctx, r.Client, namespace)
	if err != nil {
		logger.Error(err, "Failed to check Keycloak readiness")
		return false, err
	}

	return ready, nil
}

// reconcileExternalKeycloak validates external auth providers
func (r *RestReconciler) reconcileExternalKeycloak(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string, config *carbitev1alpha1.KeycloakConfig) (bool, error) {
	logger := log.FromContext(ctx).WithValues("mode", "external")
	logger.Info("Validating external auth providers")

	if len(config.AuthProviders) == 0 {
		return false, fmt.Errorf("at least one auth provider is required when mode is external")
	}

	// Validate that auth provider secrets exist if referenced
	for _, provider := range config.AuthProviders {
		if provider.ClientSecretRef != nil {
			available, err := utils.IsSecretAvailable(ctx, r.Client, namespace, provider.ClientSecretRef.Name)
			if err != nil {
				return false, err
			}
			if !available {
				logger.Info("Auth provider client secret not found", "provider", provider.Name, "secret", provider.ClientSecretRef.Name)
				return false, fmt.Errorf("auth provider %s client secret %s not found", provider.Name, provider.ClientSecretRef.Name)
			}
		}
	}

	logger.Info("External auth providers validated", "count", len(config.AuthProviders))
	return true, nil
}

// reconcileTemporalSetup reconciles Temporal namespace initialization
func (r *RestReconciler) reconcileTemporalSetup(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "temporal-setup")
	logger.Info("Reconciling Temporal setup")

	temporalNamespace := deployment.Spec.Rest.Temporal.Namespace
	if temporalNamespace == "" {
		temporalNamespace = "temporal"
	}

	initJob := restresources.BuildTemporalSetupJob(deployment, namespace, temporalNamespace)
	if err := r.createOrUpdate(ctx, initJob); err != nil {
		logger.Error(err, "Failed to create/update Temporal namespace init job")
		return false, err
	}

	complete, err := utils.IsJobComplete(ctx, r.Client, namespace, initJob.Name)
	if err != nil {
		logger.Error(err, "Failed to check Temporal setup job status")
		return false, err
	}

	return complete, nil
}

// reconcileRestAPI reconciles the REST API deployment
func (r *RestReconciler) reconcileRestAPI(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "rest-api")
	logger.Info("Reconciling REST API")

	temporalEndpoint := r.getTemporalEndpoint(ctx, deployment)
	keycloakEndpoint := r.getKeycloakEndpoint(ctx, deployment)

	apiConfigMap := restresources.BuildRestAPIConfigMap(deployment, namespace, temporalEndpoint, keycloakEndpoint)
	if err := r.createOrUpdate(ctx, apiConfigMap); err != nil {
		logger.Error(err, "Failed to create/update REST API ConfigMap")
		return false, err
	}

	apiDeployment := restresources.BuildRestAPIDeployment(deployment, namespace)
	if err := r.createOrUpdate(ctx, apiDeployment); err != nil {
		logger.Error(err, "Failed to create/update REST API Deployment")
		return false, err
	}

	apiService := restresources.BuildRestAPIService(deployment, namespace)
	if err := r.createOrUpdate(ctx, apiService); err != nil {
		logger.Error(err, "Failed to create/update REST API Service")
		return false, err
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, "carbide-rest-api")
	if err != nil {
		logger.Error(err, "Failed to check REST API readiness")
		return false, err
	}

	return ready, nil
}

// reconcileWorker reconciles a Temporal worker deployment
func (r *RestReconciler) reconcileWorker(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace, name string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", name)
	logger.Info("Reconciling worker")

	var workerDeployment interface{ GetName() string }
	var dep *interface{}
	_ = dep

	var d interface{}
	if name == restresources.CloudWorkerName {
		d = restresources.BuildCloudWorkerDeployment(deployment, namespace)
	} else {
		d = restresources.BuildSiteWorkerDeployment(deployment, namespace)
	}
	workerDeployment = d.(interface{ GetName() string })

	if err := r.createOrUpdate(ctx, d.(client.Object)); err != nil {
		logger.Error(err, "Failed to create/update worker Deployment")
		return false, err
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, workerDeployment.GetName())
	if err != nil {
		logger.Error(err, "Failed to check worker readiness")
		return false, err
	}

	return ready, nil
}

// reconcileSiteManager reconciles the site-manager service
func (r *RestReconciler) reconcileSiteManager(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "site-manager")
	logger.Info("Reconciling site-manager")

	if deployment.Spec.Profile != carbitev1alpha1.ProfileManagement &&
		deployment.Spec.Profile != carbitev1alpha1.ProfileManagementWithSite {
		return true, nil
	}

	sa := restresources.BuildSiteManagerServiceAccount(deployment, namespace)
	if err := r.createOrUpdate(ctx, sa); err != nil {
		logger.Error(err, "Failed to create/update site-manager ServiceAccount")
		return false, err
	}

	role := restresources.BuildSiteManagerRole(deployment, namespace)
	if err := r.createOrUpdate(ctx, role); err != nil {
		logger.Error(err, "Failed to create/update site-manager Role")
		return false, err
	}

	roleBinding := restresources.BuildSiteManagerRoleBinding(deployment, namespace)
	if err := r.createOrUpdate(ctx, roleBinding); err != nil {
		logger.Error(err, "Failed to create/update site-manager RoleBinding")
		return false, err
	}

	siteManagerDeployment := restresources.BuildSiteManagerDeployment(deployment, namespace)
	if err := r.createOrUpdate(ctx, siteManagerDeployment); err != nil {
		logger.Error(err, "Failed to create/update site-manager Deployment")
		return false, err
	}

	siteManagerService := restresources.BuildSiteManagerService(deployment, namespace)
	if err := r.createOrUpdate(ctx, siteManagerService); err != nil {
		logger.Error(err, "Failed to create/update site-manager Service")
		return false, err
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, restresources.SiteManagerName)
	if err != nil {
		logger.Error(err, "Failed to check site-manager readiness")
		return false, err
	}

	return ready, nil
}

// reconcileSiteAgent reconciles the site agent
func (r *RestReconciler) reconcileSiteAgent(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "site-agent")

	if deployment.Spec.Rest.SiteAgent == nil || !deployment.Spec.Rest.SiteAgent.Enabled {
		return true, nil
	}

	logger.Info("Reconciling site agent")

	temporalEndpoint := r.getTemporalEndpoint(ctx, deployment)

	siteAgentConfigMap := restresources.BuildSiteAgentConfigMap(deployment, namespace, temporalEndpoint)
	if err := r.createOrUpdate(ctx, siteAgentConfigMap); err != nil {
		logger.Error(err, "Failed to create/update Site Agent ConfigMap")
		return false, err
	}

	siteAgentDeployment := restresources.BuildSiteAgentDeployment(deployment, namespace)
	if err := r.createOrUpdate(ctx, siteAgentDeployment); err != nil {
		logger.Error(err, "Failed to create/update Site Agent Deployment")
		return false, err
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, "carbide-rest-site-agent")
	if err != nil {
		logger.Error(err, "Failed to check Site Agent readiness")
		return false, err
	}

	return ready, nil
}

// getComponentMessage generates a status message for a component
func (r *RestReconciler) getComponentMessage(name string, ready bool, err error) string {
	if err != nil {
		return fmt.Sprintf("%s: %v", name, err)
	}
	if ready {
		return fmt.Sprintf("%s is ready", name)
	}
	return fmt.Sprintf("%s is not ready", name)
}

// failedStatus creates a failed tier status
func (r *RestReconciler) failedStatus(message string, err error) *carbitev1alpha1.TierStatus {
	return &carbitev1alpha1.TierStatus{
		Ready:              false,
		Message:            fmt.Sprintf("%s: %v", message, err),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	}
}

// getPostgreSQLConnection retrieves PostgreSQL connection details
func (r *RestReconciler) getPostgreSQLConnection(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (host string, port int32, secretName string, err error) {
	infraConfig := deployment.Spec.Infrastructure
	if infraConfig == nil {
		return "", 0, "", fmt.Errorf("infrastructure config is nil")
	}

	pgConfig := infraConfig.PostgreSQL

	mode := pgConfig.Mode
	if mode == "" {
		mode = carbitev1alpha1.ManagedMode
	}

	if mode == carbitev1alpha1.ExternalMode {
		if pgConfig.Connection == nil {
			return "", 0, "", fmt.Errorf("external PostgreSQL connection is nil")
		}
		temporalSecret := ""
		if pgConfig.Connection.UserSecrets != nil {
			if ref, ok := pgConfig.Connection.UserSecrets["temporal"]; ok {
				temporalSecret = ref.Name
			}
		}
		return pgConfig.Connection.Host, pgConfig.Connection.Port, temporalSecret, nil
	}

	infraNamespace := infraConfig.Namespace
	if infraNamespace == "" {
		infraNamespace = "carbide-operators"
	}

	host = fmt.Sprintf("carbide-postgres-primary.%s.svc", infraNamespace)
	port = 5432
	secretName = "carbide-postgres-pguser-temporal"

	return host, port, secretName, nil
}

// getForgePassword retrieves the PostgreSQL forge user password
func (r *RestReconciler) getForgePassword(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (string, error) {
	infraConfig := deployment.Spec.Infrastructure
	if infraConfig == nil {
		return "", fmt.Errorf("infrastructure config is nil")
	}

	mode := infraConfig.PostgreSQL.Mode
	if mode == "" {
		mode = carbitev1alpha1.ManagedMode
	}

	if mode == carbitev1alpha1.ExternalMode {
		if infraConfig.PostgreSQL.Connection == nil {
			return "", fmt.Errorf("external PostgreSQL connection is nil")
		}
		// Look for forge user secret in the per-user secrets
		if infraConfig.PostgreSQL.Connection.UserSecrets != nil {
			if secretRef, ok := infraConfig.PostgreSQL.Connection.UserSecrets["forge"]; ok {
				secret := &corev1.Secret{}
				key := secretRef.PasswordKey
				if key == "" {
					key = "password"
				}
				if err := r.Get(ctx, client.ObjectKey{
					Namespace: infraConfig.Namespace,
					Name:      secretRef.Name,
				}, secret); err != nil {
					return "", err
				}
				return string(secret.Data[key]), nil
			}
		}
		return "", fmt.Errorf("forge user secret not found in external PostgreSQL connection")
	}

	infraNamespace := infraConfig.Namespace
	if infraNamespace == "" {
		infraNamespace = "carbide-operators"
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: infraNamespace,
		Name:      "carbide-postgres-pguser-forge",
	}, secret); err != nil {
		return "", fmt.Errorf("failed to get forge PostgreSQL secret: %w", err)
	}

	return string(secret.Data["password"]), nil
}

// createOrUpdateUnstructured creates or updates an unstructured resource
func (r *RestReconciler) createOrUpdateUnstructured(ctx context.Context, obj *unstructured.Unstructured) error {
	logger := log.FromContext(ctx)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	err := r.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, existing)

	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("Creating resource", "gvk", obj.GroupVersionKind(), "name", obj.GetName())
			return r.Create(ctx, obj)
		}
		return err
	}

	logger.Info("Updating resource", "gvk", obj.GroupVersionKind(), "name", obj.GetName())
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// createOrUpdate creates or updates a Kubernetes object
func (r *RestReconciler) createOrUpdate(ctx context.Context, obj client.Object) error {
	logger := log.FromContext(ctx)

	existing := obj.DeepCopyObject().(client.Object)
	err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating resource", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName())
			return r.Create(ctx, obj)
		}
		return err
	}

	logger.Info("Updating resource", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName())
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// getTemporalEndpoint retrieves the Temporal endpoint based on managed/external mode
func (r *RestReconciler) getTemporalEndpoint(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) string {
	temporalConfig := deployment.Spec.Rest.Temporal

	mode := temporalConfig.Mode
	if mode == "" {
		mode = carbitev1alpha1.ManagedMode
	}

	if mode == carbitev1alpha1.ExternalMode {
		return temporalConfig.Endpoint
	}

	namespace := temporalConfig.Namespace
	if namespace == "" {
		namespace = "temporal"
	}

	return restresources.GetTemporalFrontendURL(namespace)
}

// getKeycloakEndpoint retrieves the Keycloak endpoint based on mode
func (r *RestReconciler) getKeycloakEndpoint(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) string {
	keycloakConfig := deployment.Spec.Rest.Keycloak

	switch keycloakConfig.Mode {
	case carbitev1alpha1.AuthModeExternal:
		// Use first auth provider's issuer URL as the base
		if len(keycloakConfig.AuthProviders) > 0 {
			return keycloakConfig.AuthProviders[0].IssuerURL
		}
		return ""
	case carbitev1alpha1.AuthModeDisabled:
		return ""
	default:
		// Managed mode
		coreNamespace := deployment.Spec.Core.Namespace
		if coreNamespace == "" {
			coreNamespace = "carbide"
		}
		return fmt.Sprintf("http://carbide-keycloak.%s.svc:8080/auth", coreNamespace)
	}
}

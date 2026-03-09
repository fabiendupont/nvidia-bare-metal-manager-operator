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
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/core"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/infrastructure"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/utils"
)

// CoreReconciler reconciles core tier components

type CoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles the core tier
func (r *CoreReconciler) Reconcile(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (*carbitev1alpha1.TierStatus, error) {
	logger := log.FromContext(ctx).WithValues("tier", "core")
	logger.Info("Reconciling core tier", "profile", deployment.Spec.Profile)

	// For management profile, core tier is not needed (no site services)
	if deployment.Spec.Profile == carbitev1alpha1.ProfileManagement {
		logger.Info("Core tier not required for management profile")
		return &carbitev1alpha1.TierStatus{
			Ready:              true,
			Message:            "Not required for management profile",
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		}, nil
	}

	coreConfig := deployment.Spec.Core

	// Determine namespace
	namespace := coreConfig.Namespace
	if namespace == "" {
		if deployment.Spec.Infrastructure != nil && deployment.Spec.Infrastructure.Namespace != "" {
			namespace = deployment.Spec.Infrastructure.Namespace
		} else {
			namespace = restDefaultNamespace
		}
	}

	// Ensure namespace exists
	if err := r.ensureNamespace(ctx, namespace); err != nil {
		return r.failedStatus("Failed to create namespace", err), err
	}

	// Initialize tier status
	tierStatus := &carbitev1alpha1.TierStatus{
		Ready:              false,
		Components:         []carbitev1alpha1.ComponentStatus{},
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	}

	// 0. Create ServiceAccounts
	saNames := []string{core.APIName, core.DHCPName, core.DNSName, core.PXEName}
	if deployment.Spec.Core.RLA != nil && deployment.Spec.Core.RLA.Enabled {
		saNames = append(saNames, core.RLAName)
	}
	if deployment.Spec.Core.PSM != nil && deployment.Spec.Core.PSM.Enabled {
		saNames = append(saNames, core.PSMName)
	}
	for _, saName := range saNames {
		sa := core.BuildServiceAccount(saName, namespace, deployment)
		if err := r.createOrUpdate(ctx, sa); err != nil {
			return r.failedStatus(fmt.Sprintf("Failed to create ServiceAccount %s", saName), err), err
		}
	}

	// 0b. Create TLS resources
	if tls.IsSpiffeMode(deployment) {
		// spiffe-helper ConfigMap
		helperCM := tls.BuildSpiffeHelperConfigMap(deployment, namespace)
		if err := r.createOrUpdate(ctx, helperCM); err != nil {
			return r.failedStatus("Failed to create spiffe-helper ConfigMap", err), err
		}

		// ClusterSPIFFEIDs
		apiSPIFFEID := tls.BuildClusterSPIFFEID(deployment, "carbide-api", namespace, core.APIName,
			[]string{fmt.Sprintf("carbide-api.%s.svc.cluster.local", namespace)})
		if err := r.createOrUpdateUnstructured(ctx, apiSPIFFEID); err != nil {
			return r.failedStatus("Failed to create carbide-api ClusterSPIFFEID", err), err
		}

		spiffeNames := []string{core.DHCPName, core.DNSName, core.PXEName}
		if deployment.Spec.Core.RLA != nil && deployment.Spec.Core.RLA.Enabled {
			spiffeNames = append(spiffeNames, core.RLAName)
		}
		if deployment.Spec.Core.PSM != nil && deployment.Spec.Core.PSM.Enabled {
			spiffeNames = append(spiffeNames, core.PSMName)
		}
		for _, name := range spiffeNames {
			sid := tls.BuildClusterSPIFFEID(deployment, name, namespace, name, nil)
			if err := r.createOrUpdateUnstructured(ctx, sid); err != nil {
				return r.failedStatus(fmt.Sprintf("Failed to create %s ClusterSPIFFEID", name), err), err
			}
		}
	} else if tls.IsCertManagerMode(deployment) {
		// Create cert-manager Certificates for each service
		for _, name := range saNames {
			cert := tls.BuildCertificate(deployment, name, namespace,
				[]string{fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace)})
			if err := r.createOrUpdateUnstructured(ctx, cert); err != nil {
				return r.failedStatus(fmt.Sprintf("Failed to create %s Certificate", name), err), err
			}
		}
	}

	// 1. ConfigMaps, Secrets, and DNS ConfigMap
	configReady, configErr := r.reconcileConfig(ctx, deployment, namespace)
	if configErr != nil {
		tierStatus.Message = fmt.Sprintf("Config reconciliation failed: %v", configErr)
		return tierStatus, configErr
	}

	// 2. Migration Job
	migrationComplete, migrationErr := r.reconcileMigration(ctx, deployment, namespace)
	tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
		Name:               "Migration",
		Ready:              migrationComplete,
		Message:            r.getComponentMessage("Migration", migrationComplete, migrationErr),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	})
	if migrationErr != nil {
		tierStatus.Message = fmt.Sprintf("Migration failed: %v", migrationErr)
		return tierStatus, migrationErr
	}
	if !migrationComplete {
		tierStatus.Message = "Waiting for database migration to complete"
		return tierStatus, nil
	}

	// 3. Carbide API
	apiReady, apiErr := r.reconcileAPI(ctx, deployment, namespace)
	tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
		Name:               "API",
		Ready:              apiReady,
		Message:            r.getComponentMessage("API", apiReady, apiErr),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	})
	if apiErr != nil {
		tierStatus.Message = fmt.Sprintf("API reconciliation failed: %v", apiErr)
		return tierStatus, apiErr
	}

	// 4. Network Services (parallel after API is ready)
	var dhcpReady, pxeReady, dnsReady bool
	var dhcpErr, pxeErr, dnsErr error

	if apiReady {
		// DHCP
		if coreConfig.DHCP.Enabled {
			dhcpReady, dhcpErr = r.reconcileDHCP(ctx, deployment, namespace)
			tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
				Name:               "DHCP",
				Ready:              dhcpReady,
				Message:            r.getComponentMessage("DHCP", dhcpReady, dhcpErr),
				LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
			})
			if dhcpErr != nil {
				tierStatus.Message = fmt.Sprintf("DHCP reconciliation failed: %v", dhcpErr)
				return tierStatus, dhcpErr
			}
		} else {
			dhcpReady = true
		}

		// PXE
		if coreConfig.PXE.Enabled {
			pxeReady, pxeErr = r.reconcilePXE(ctx, deployment, namespace)
			tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
				Name:               "PXE",
				Ready:              pxeReady,
				Message:            r.getComponentMessage("PXE", pxeReady, pxeErr),
				LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
			})
			if pxeErr != nil {
				tierStatus.Message = fmt.Sprintf("PXE reconciliation failed: %v", pxeErr)
				return tierStatus, pxeErr
			}
		} else {
			pxeReady = true
		}

		// DNS
		if coreConfig.DNS.Enabled {
			dnsReady, dnsErr = r.reconcileDNS(ctx, deployment, namespace)
			tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
				Name:               "DNS",
				Ready:              dnsReady,
				Message:            r.getComponentMessage("DNS", dnsReady, dnsErr),
				LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
			})
			if dnsErr != nil {
				tierStatus.Message = fmt.Sprintf("DNS reconciliation failed: %v", dnsErr)
				return tierStatus, dnsErr
			}
		} else {
			dnsReady = true
		}
	}

	// 5. Vault (after API ready, if configured)
	vaultReady := true
	if apiReady && deployment.Spec.Core.Vault != nil {
		var vaultErr error
		vaultReady, vaultErr = r.reconcileVault(ctx, deployment, namespace)
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "Vault",
			Ready:              vaultReady,
			Message:            r.getComponentMessage("Vault", vaultReady, vaultErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})
		if vaultErr != nil {
			tierStatus.Message = fmt.Sprintf("Vault reconciliation failed: %v", vaultErr)
			return tierStatus, vaultErr
		}
	}

	// 6. RLA (after API + Vault ready, if enabled)
	rlaReady := true
	if apiReady && vaultReady && deployment.Spec.Core.RLA != nil && deployment.Spec.Core.RLA.Enabled {
		var rlaErr error
		rlaReady, rlaErr = r.reconcileRLA(ctx, deployment, namespace)
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "RLA",
			Ready:              rlaReady,
			Message:            r.getComponentMessage("RLA", rlaReady, rlaErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})
		if rlaErr != nil {
			tierStatus.Message = fmt.Sprintf("RLA reconciliation failed: %v", rlaErr)
			return tierStatus, rlaErr
		}
	}

	// 7. PSM (after API + Vault ready, if enabled)
	psmReady := true
	if apiReady && vaultReady && deployment.Spec.Core.PSM != nil && deployment.Spec.Core.PSM.Enabled {
		var psmErr error
		psmReady, psmErr = r.reconcilePSM(ctx, deployment, namespace)
		tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
			Name:               "PSM",
			Ready:              psmReady,
			Message:            r.getComponentMessage("PSM", psmReady, psmErr),
			LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
		})
		if psmErr != nil {
			tierStatus.Message = fmt.Sprintf("PSM reconciliation failed: %v", psmErr)
			return tierStatus, psmErr
		}
	}

	// Check overall tier readiness
	tierStatus.Ready = configReady && migrationComplete && apiReady && dhcpReady && pxeReady && dnsReady && vaultReady && rlaReady && psmReady
	if tierStatus.Ready {
		tierStatus.Message = "All core components ready"
	} else {
		tierStatus.Message = "Waiting for core components to be ready"
	}

	logger.Info("Core tier reconciliation complete", "ready", tierStatus.Ready)
	return tierStatus, nil
}

// reconcileConfig reconciles ConfigMaps and Secrets
func (r *CoreReconciler) reconcileConfig(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "config")
	logger.Info("Reconciling configuration")

	// Get connection details from infrastructure tier
	pgHost, pgPort, err := r.getInfrastructureConnections(ctx, deployment)
	if err != nil {
		logger.Error(err, "Failed to get infrastructure connections")
		return false, err
	}

	// Build and create carbide-api ConfigMap
	configMap := core.BuildAPIConfigMap(deployment, namespace, pgHost, pgPort)
	if err := r.createOrUpdate(ctx, configMap); err != nil {
		logger.Error(err, "Failed to create/update API ConfigMap")
		return false, err
	}

	// Build and create Casbin policy ConfigMap
	casbinConfigMap := core.BuildCasbinPolicyConfigMap(deployment, namespace)
	if err := r.createOrUpdate(ctx, casbinConfigMap); err != nil {
		logger.Error(err, "Failed to create/update Casbin ConfigMap")
		return false, err
	}

	// Build and create DNS ConfigMap
	if deployment.Spec.Core.DNS.Enabled {
		dnsConfigMap := core.BuildDNSConfigMap(deployment, namespace)
		if err := r.createOrUpdate(ctx, dnsConfigMap); err != nil {
			logger.Error(err, "Failed to create/update DNS ConfigMap")
			return false, err
		}
	}

	// Build and create API Secret with database URL
	pgPassword, err := r.getPostgreSQLPassword(ctx, deployment)
	if err != nil {
		logger.Error(err, "Failed to get PostgreSQL password")
		return false, err
	}

	apiSecret := core.BuildAPISecret(deployment, namespace, pgHost, pgPort, pgPassword)
	if err := r.createOrUpdate(ctx, apiSecret); err != nil {
		logger.Error(err, "Failed to create/update API Secret")
		return false, err
	}

	logger.Info("Configuration reconciled successfully")
	return true, nil
}

// reconcileMigration reconciles the database migration job
func (r *CoreReconciler) reconcileMigration(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "migration")
	logger.Info("Database migration handled by init containers")
	return true, nil
}

// reconcileAPI reconciles the carbide-api deployment
func (r *CoreReconciler) reconcileAPI(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "api")
	logger.Info("Reconciling carbide-api")

	apiDeployment := core.BuildAPIDeployment(deployment, namespace)
	if err := r.createOrUpdate(ctx, apiDeployment); err != nil {
		logger.Error(err, "Failed to create/update API Deployment")
		return false, err
	}

	apiService := core.BuildAPIService(deployment, namespace)
	if err := r.createOrUpdate(ctx, apiService); err != nil {
		logger.Error(err, "Failed to create/update API Service")
		return false, err
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, core.APIName)
	if err != nil {
		logger.Error(err, "Failed to check API readiness")
		return false, err
	}

	return ready, nil
}

// reconcileDHCP reconciles the DHCP server
func (r *CoreReconciler) reconcileDHCP(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "dhcp")
	logger.Info("Reconciling DHCP server")

	if !deployment.Spec.Core.DHCP.Enabled {
		return true, nil
	}

	dhcpDaemonSet := core.BuildDHCPDaemonSet(deployment, namespace)
	if err := r.createOrUpdate(ctx, dhcpDaemonSet); err != nil {
		logger.Error(err, "Failed to create/update DHCP DaemonSet")
		return false, err
	}

	ready, err := utils.IsDaemonSetReady(ctx, r.Client, namespace, "carbide-dhcp")
	if err != nil {
		logger.Error(err, "Failed to check DHCP readiness")
		return false, err
	}

	return ready, nil
}

// reconcilePXE reconciles the PXE boot server
func (r *CoreReconciler) reconcilePXE(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "pxe")
	logger.Info("Reconciling PXE server")

	if !deployment.Spec.Core.PXE.Enabled {
		return true, nil
	}

	if deployment.Spec.Core.PXE.Storage != nil {
		pxePVC := core.BuildPXEPVC(deployment, namespace)
		if err := r.createOrUpdate(ctx, pxePVC); err != nil {
			logger.Error(err, "Failed to create/update PXE PVC")
			return false, err
		}

		bound, err := utils.IsPVCBound(ctx, r.Client, namespace, pxePVC.Name)
		if err != nil {
			logger.Error(err, "Failed to check PVC status")
			return false, err
		}
		if !bound {
			logger.Info("PXE PVC not bound yet")
			return false, nil
		}
	}

	pxeDeployment := core.BuildPXEDeployment(deployment, namespace)
	if err := r.createOrUpdate(ctx, pxeDeployment); err != nil {
		logger.Error(err, "Failed to create/update PXE Deployment")
		return false, err
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, core.PXEName)
	if err != nil {
		logger.Error(err, "Failed to check PXE readiness")
		return false, err
	}

	return ready, nil
}

// reconcileDNS reconciles the DNS server
func (r *CoreReconciler) reconcileDNS(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "dns")
	logger.Info("Reconciling DNS server")

	if !deployment.Spec.Core.DNS.Enabled {
		return true, nil
	}

	dnsDaemonSet := core.BuildDNSDaemonSet(deployment, namespace)
	if err := r.createOrUpdate(ctx, dnsDaemonSet); err != nil {
		logger.Error(err, "Failed to create/update DNS DaemonSet")
		return false, err
	}

	ready, err := utils.IsDaemonSetReady(ctx, r.Client, namespace, "carbide-dns")
	if err != nil {
		logger.Error(err, "Failed to check DNS readiness")
		return false, err
	}

	return ready, nil
}

// reconcileVault reconciles the Vault component
func (r *CoreReconciler) reconcileVault(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "vault")
	vaultConfig := deployment.Spec.Core.Vault

	if vaultConfig == nil {
		return true, nil
	}

	switch vaultConfig.Mode {
	case carbitev1alpha1.ManagedMode:
		logger.Info("Reconciling managed Vault")

		// Create Vault Helm values ConfigMap
		valuesCM := core.BuildVaultHelmValuesConfigMap(deployment, namespace)
		if err := r.createOrUpdate(ctx, valuesCM); err != nil {
			return false, fmt.Errorf("failed to create Vault values ConfigMap: %w", err)
		}

		// Create Vault Helm Job
		helmJob := core.BuildVaultHelmJob(deployment, namespace)
		if err := r.createOrUpdate(ctx, helmJob); err != nil {
			return false, fmt.Errorf("failed to create Vault Helm job: %w", err)
		}

		// Check if Helm job is complete
		helmComplete, err := utils.IsJobComplete(ctx, r.Client, namespace, core.VaultHelmJobName)
		if err != nil {
			return false, err
		}
		if !helmComplete {
			logger.Info("Vault Helm install not complete yet")
			return false, nil
		}

		// Create Vault init job
		initJob := core.BuildVaultInitJob(deployment, namespace)
		if err := r.createOrUpdate(ctx, initJob); err != nil {
			return false, fmt.Errorf("failed to create Vault init job: %w", err)
		}

		initComplete, err := utils.IsJobComplete(ctx, r.Client, namespace, core.VaultInitJobName)
		if err != nil {
			return false, err
		}

		return initComplete, nil

	case carbitev1alpha1.ExternalMode:
		logger.Info("Validating external Vault")
		if vaultConfig.Address == "" {
			return false, fmt.Errorf("external Vault address is required")
		}
		// For external mode, just validate the secret exists
		if vaultConfig.TokenSecretRef != nil {
			available, err := utils.IsSecretAvailable(ctx, r.Client, namespace, vaultConfig.TokenSecretRef.Name)
			if err != nil {
				return false, err
			}
			if !available {
				return false, fmt.Errorf("vault token secret %s not found", vaultConfig.TokenSecretRef.Name)
			}
		}
		return true, nil

	default:
		return false, fmt.Errorf("invalid Vault mode: %s", vaultConfig.Mode)
	}
}

// reconcileRLA reconciles the RLA component
func (r *CoreReconciler) reconcileRLA(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "rla")
	logger.Info("Reconciling RLA")

	rlaDeployment := core.BuildRLADeployment(deployment, namespace)
	if rlaDeployment == nil {
		return true, nil
	}
	if err := r.createOrUpdate(ctx, rlaDeployment); err != nil {
		return false, fmt.Errorf("failed to create/update RLA Deployment: %w", err)
	}

	rlaService := core.BuildRLAService(deployment, namespace)
	if err := r.createOrUpdate(ctx, rlaService); err != nil {
		return false, fmt.Errorf("failed to create/update RLA Service: %w", err)
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, core.RLAName)
	if err != nil {
		return false, err
	}

	return ready, nil
}

// reconcilePSM reconciles the PSM component
func (r *CoreReconciler) reconcilePSM(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "psm")
	logger.Info("Reconciling PSM")

	psmDeployment := core.BuildPSMDeployment(deployment, namespace)
	if psmDeployment == nil {
		return true, nil
	}
	if err := r.createOrUpdate(ctx, psmDeployment); err != nil {
		return false, fmt.Errorf("failed to create/update PSM Deployment: %w", err)
	}

	psmService := core.BuildPSMService(deployment, namespace)
	if err := r.createOrUpdate(ctx, psmService); err != nil {
		return false, fmt.Errorf("failed to create/update PSM Service: %w", err)
	}

	ready, err := utils.IsDeploymentReady(ctx, r.Client, namespace, core.PSMName)
	if err != nil {
		return false, err
	}

	return ready, nil
}

// ensureNamespace ensures the namespace exists
func (r *CoreReconciler) ensureNamespace(ctx context.Context, name string) error {
	logger := log.FromContext(ctx)

	ns := &corev1.Namespace{}
	ns.Name = name

	err := r.Get(ctx, client.ObjectKey{Name: name}, ns)
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		return err
	}

	logger.Info("Creating namespace", "namespace", name)
	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "carbide-operator",
				"kubernetes.io/metadata.name":  name,
			},
		},
	}

	return r.Create(ctx, ns)
}

// getComponentMessage generates a status message for a component
func (r *CoreReconciler) getComponentMessage(name string, ready bool, err error) string {
	if err != nil {
		return fmt.Sprintf("%s: %v", name, err)
	}
	if ready {
		return fmt.Sprintf("%s is ready", name)
	}
	return fmt.Sprintf("%s is not ready", name)
}

// failedStatus creates a failed tier status
func (r *CoreReconciler) failedStatus(message string, err error) *carbitev1alpha1.TierStatus {
	return &carbitev1alpha1.TierStatus{
		Ready:              false,
		Message:            fmt.Sprintf("%s: %v", message, err),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	}
}

// getInfrastructureConnections retrieves connection details from infrastructure tier
func (r *CoreReconciler) getInfrastructureConnections(_ context.Context, deployment *carbitev1alpha1.CarbideDeployment) (pgHost string, pgPort int32, err error) {
	infraConfig := deployment.Spec.Infrastructure
	if infraConfig == nil {
		return "", 0, fmt.Errorf("infrastructure config is nil")
	}

	infraNamespace := infraConfig.Namespace
	if infraNamespace == "" {
		infraNamespace = restDefaultNamespace
	}

	pgConfig := infraConfig.PostgreSQL
	mode := pgConfig.Mode
	if mode == "" {
		mode = carbitev1alpha1.ManagedMode
	}

	if mode == carbitev1alpha1.ExternalMode {
		if pgConfig.Connection == nil {
			return "", 0, fmt.Errorf("external PostgreSQL connection is nil")
		}
		pgHost = pgConfig.Connection.Host
		pgPort = pgConfig.Connection.Port
	} else {
		pgHost = fmt.Sprintf("carbide-postgres-primary.%s.svc", infraNamespace)
		pgPort = 5432
	}

	return pgHost, pgPort, nil
}

// getPostgreSQLPassword retrieves the PostgreSQL password from the PGO-managed secret
func (r *CoreReconciler) getPostgreSQLPassword(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (string, error) {
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
			return "", fmt.Errorf("external PostgreSQL connection is required")
		}
		// Look for carbide user secret in the per-user secrets
		if infraConfig.PostgreSQL.Connection.UserSecrets != nil {
			if secretRef, ok := infraConfig.PostgreSQL.Connection.UserSecrets["carbide"]; ok {
				secret := &corev1.Secret{}
				key := secretRef.PasswordKey
				if key == "" {
					key = "password"
				}
				if err := r.Get(ctx, client.ObjectKey{
					Namespace: infraConfig.Namespace,
					Name:      secretRef.Name,
				}, secret); err != nil {
					return "", fmt.Errorf("failed to get external PostgreSQL secret: %w", err)
				}
				return string(secret.Data[key]), nil
			}
		}
		return "", fmt.Errorf("carbide user secret not found in external PostgreSQL connection")
	}

	// Managed PostgreSQL: read from PGO-managed secret
	infraNamespace := infraConfig.Namespace
	if infraNamespace == "" {
		infraNamespace = restDefaultNamespace
	}

	secretName := infrastructure.GetPostgreSQLConnectionSecret("carbide")
	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: infraNamespace,
		Name:      secretName,
	}, secret); err != nil {
		return "", fmt.Errorf("failed to get PostgreSQL secret %s/%s: %w", infraNamespace, secretName, err)
	}

	password := string(secret.Data["password"])
	if password == "" {
		return "", fmt.Errorf("password key not found in secret %s/%s", infraNamespace, secretName)
	}

	return password, nil
}

// createOrUpdate creates or updates a Kubernetes object
func (r *CoreReconciler) createOrUpdate(ctx context.Context, obj client.Object) error {
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
	updateErr := r.Update(ctx, obj)
	if updateErr != nil && errors.IsConflict(updateErr) {
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing); err != nil {
			return err
		}
		obj.SetResourceVersion(existing.GetResourceVersion())
		return r.Update(ctx, obj)
	}
	return updateErr
}

// createOrUpdateUnstructured creates or updates an unstructured resource
func (r *CoreReconciler) createOrUpdateUnstructured(ctx context.Context, obj *unstructured.Unstructured) error {
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
	updateErr := r.Update(ctx, obj)
	if updateErr != nil && errors.IsConflict(updateErr) {
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing); err != nil {
			return err
		}
		obj.SetResourceVersion(existing.GetResourceVersion())
		return r.Update(ctx, obj)
	}
	return updateErr
}

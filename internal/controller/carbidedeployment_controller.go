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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/conditions"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/tls"
)

const (
	carbideFinalizer = "carbide.nvidia.com/finalizer"
	requeueDelay     = 30 * time.Second
)

// CarbideDeploymentReconciler reconciles a CarbideDeployment object
type CarbideDeploymentReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Tier reconcilers
	InfrastructureReconciler *InfrastructureReconciler
	CoreReconciler           *CoreReconciler
	RestReconciler           *RestReconciler
}

// +kubebuilder:rbac:groups=carbide.nvidia.com,resources=carbidedeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=carbide.nvidia.com,resources=carbidedeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=carbide.nvidia.com,resources=carbidedeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=postgres-operator.crunchydata.com,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8s.keycloak.org,resources=keycloaks;keycloakrealmimports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=spire.spiffe.io,resources=clusterspiffeids,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;create;update;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;create;update;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;create;update;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;create;update;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *CarbideDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling CarbideDeployment")

	// 1. Fetch CarbideDeployment
	deployment := &carbitev1alpha1.CarbideDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("CarbideDeployment not found, probably deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get CarbideDeployment")
		return ctrl.Result{}, err
	}

	// 2. Handle deletion
	if !deployment.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, deployment)
	}

	// 3. Add finalizer if not present
	if !controllerutil.ContainsFinalizer(deployment, carbideFinalizer) {
		controllerutil.AddFinalizer(deployment, carbideFinalizer)
		if err := r.Update(ctx, deployment); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Initialize status if needed
	if deployment.Status.Phase == "" {
		conditions.InitializeConditions(deployment)
		if err := r.Status().Update(ctx, deployment); err != nil {
			// Conflict is expected on first reconcile — requeue and retry
			logger.Info("Status init conflict, will retry", "error", err)
			return ctrl.Result{Requeue: true}, nil
		}
		r.Recorder.Eventf(deployment, corev1.EventTypeNormal, "Initializing", "Starting %s deployment", deployment.Spec.Profile)
		return ctrl.Result{Requeue: true}, nil
	}

	// 5. Detect spec changes and transition to Updating phase
	if deployment.Status.ObservedGeneration != 0 && deployment.Status.ObservedGeneration < deployment.Generation && deployment.Status.Phase == carbitev1alpha1.PhaseReady {
		deployment.Status.Phase = carbitev1alpha1.PhaseUpdating
		r.Recorder.Event(deployment, corev1.EventTypeNormal, "Updating", "Spec changed, reconciling updates")
	}

	deployment.Status.ObservedGeneration = deployment.Generation

	// 6. TLS prerequisite check
	if deployment.Spec.TLS != nil {
		tlsAvailable, tlsErr := r.checkTLSPrerequisites(ctx, deployment)
		if tlsErr != nil {
			logger.Error(tlsErr, "TLS prerequisite check failed")
		}
		if !tlsAvailable {
			deployment.Status.Phase = carbitev1alpha1.PhaseFailed
			conditions.SetReadyCondition(deployment)
			r.Recorder.Eventf(deployment, corev1.EventTypeWarning, "TLSUnavailable", "TLS backend %s not available", deployment.Spec.TLS.Mode)
			// Re-fetch to avoid conflict
			latest := &carbitev1alpha1.CarbideDeployment{}
			if getErr := r.Get(ctx, req.NamespacedName, latest); getErr == nil {
				latest.Status = deployment.Status
				if statusErr := r.Status().Update(ctx, latest); statusErr != nil {
					logger.Info("Status update conflict on TLS check, will retry", "error", statusErr)
				}
			}
			return ctrl.Result{RequeueAfter: requeueDelay}, nil
		}
	}

	// 7. Reconcile tiers in order
	var requeueRequired bool
	var err error

	// Infrastructure tier (optional)
	if deployment.Spec.Infrastructure != nil {
		logger.Info("Reconciling Infrastructure tier")
		infraReady, infraErr := r.reconcileInfrastructure(ctx, deployment)
		if infraErr != nil {
			err = infraErr
			logger.Error(err, "Failed to reconcile Infrastructure tier")
			deployment.Status.Phase = carbitev1alpha1.PhaseFailed
			r.Recorder.Eventf(deployment, corev1.EventTypeWarning, "InfrastructureFailed", "Infrastructure tier failed: %v", err)
		} else if !infraReady {
			requeueRequired = true
			logger.Info("Infrastructure tier not ready yet")
		}
	}

	// Core tier (required) - only reconcile if infrastructure is ready (or not configured)
	infraReady := deployment.Spec.Infrastructure == nil ||
		(deployment.Status.Infrastructure != nil && deployment.Status.Infrastructure.Ready)

	if infraReady {
		logger.Info("Reconciling Core tier")
		coreReady, coreErr := r.reconcileCore(ctx, deployment)
		if coreErr != nil {
			err = coreErr
			logger.Error(err, "Failed to reconcile Core tier")
			deployment.Status.Phase = carbitev1alpha1.PhaseFailed
			r.Recorder.Eventf(deployment, corev1.EventTypeWarning, "CoreFailed", "Core tier failed: %v", err)
		} else if !coreReady {
			requeueRequired = true
			logger.Info("Core tier not ready yet")
		}
	}

	// Rest tier (optional) - only reconcile if core is ready
	coreReady := deployment.Status.Core != nil && deployment.Status.Core.Ready

	if deployment.Spec.Rest != nil && deployment.Spec.Rest.Enabled && coreReady {
		logger.Info("Reconciling Rest tier")
		restReady, restErr := r.reconcileRest(ctx, deployment)
		if restErr != nil {
			err = restErr
			logger.Error(err, "Failed to reconcile Rest tier")
			deployment.Status.Phase = carbitev1alpha1.PhaseFailed
			r.Recorder.Eventf(deployment, corev1.EventTypeWarning, "RestFailed", "REST tier failed: %v", err)
		} else if !restReady {
			requeueRequired = true
			logger.Info("Rest tier not ready yet")
		}
	}

	// 8. Update overall conditions
	previousPhase := deployment.Status.Phase
	conditions.SetReadyCondition(deployment)

	// Record event on phase transitions
	if deployment.Status.Phase == carbitev1alpha1.PhaseReady && previousPhase != carbitev1alpha1.PhaseReady {
		r.Recorder.Event(deployment, corev1.EventTypeNormal, "Ready", "All components are ready")
	}

	// 9. Update status — re-fetch to avoid conflict with concurrent modifications
	// (e.g., webhook defaulting updates the object while we were reconciling)
	latest := &carbitev1alpha1.CarbideDeployment{}
	if statusErr := r.Get(ctx, req.NamespacedName, latest); statusErr != nil {
		logger.Error(statusErr, "Failed to re-fetch for status update")
		return ctrl.Result{}, statusErr
	}
	latest.Status = deployment.Status
	if statusErr := r.Status().Update(ctx, latest); statusErr != nil {
		logger.Error(statusErr, "Failed to update status")
		return ctrl.Result{}, statusErr
	}

	// 10. Determine result
	if err != nil {
		return ctrl.Result{RequeueAfter: requeueDelay}, err
	}

	if requeueRequired {
		logger.Info("Requeuing for pending resources", "delay", requeueDelay)
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	}

	logger.Info("Reconciliation complete", "phase", deployment.Status.Phase)
	return ctrl.Result{}, nil
}

// checkTLSPrerequisites verifies that the configured TLS backend is available.
func (r *CarbideDeploymentReconciler) checkTLSPrerequisites(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (bool, error) {
	logger := log.FromContext(ctx)

	if deployment.Spec.TLS == nil {
		return true, nil
	}

	switch deployment.Spec.TLS.Mode {
	case carbitev1alpha1.TLSModeSpiffe:
		available, err := tls.DetectSPIRE(ctx, r.Client)
		if err != nil {
			logger.Error(err, "Failed to detect SPIRE")
			conditions.SetSPIFFEAvailableCondition(deployment, false)
			conditions.SetTLSCondition(deployment, false, "Failed to detect SPIRE CSI driver")
			return false, err
		}
		conditions.SetSPIFFEAvailableCondition(deployment, available)
		if !available {
			conditions.SetTLSCondition(deployment, false, "SPIRE CSI driver not found - install SPIRE first")
			return false, nil
		}
		conditions.SetTLSCondition(deployment, true, "SPIFFE/SPIRE TLS backend available")
		return true, nil

	case carbitev1alpha1.TLSModeCertManager:
		available, err := tls.DetectCertManager(ctx, r.Client)
		if err != nil {
			logger.Error(err, "Failed to detect cert-manager")
			conditions.SetCertManagerAvailableCondition(deployment, false)
			conditions.SetTLSCondition(deployment, false, "Failed to detect cert-manager")
			return false, err
		}
		conditions.SetCertManagerAvailableCondition(deployment, available)
		if !available {
			conditions.SetTLSCondition(deployment, false, "cert-manager CRDs not found - install cert-manager first")
			return false, nil
		}
		conditions.SetTLSCondition(deployment, true, "cert-manager TLS backend available")
		return true, nil

	default:
		conditions.SetTLSCondition(deployment, false, fmt.Sprintf("Unknown TLS mode: %s", deployment.Spec.TLS.Mode))
		return false, fmt.Errorf("unknown TLS mode: %s", deployment.Spec.TLS.Mode)
	}
}

// reconcileInfrastructure reconciles the infrastructure tier
func (r *CarbideDeploymentReconciler) reconcileInfrastructure(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (bool, error) {
	if r.InfrastructureReconciler == nil {
		return false, fmt.Errorf("infrastructure reconciler not initialized")
	}

	tierStatus, err := r.InfrastructureReconciler.Reconcile(ctx, deployment)
	if err != nil {
		deployment.Status.Infrastructure = tierStatus
		conditions.SetInfrastructureCondition(deployment, tierStatus)
		return false, err
	}

	deployment.Status.Infrastructure = tierStatus
	conditions.SetInfrastructureCondition(deployment, tierStatus)

	return tierStatus != nil && tierStatus.Ready, nil
}

// reconcileCore reconciles the core tier
func (r *CarbideDeploymentReconciler) reconcileCore(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (bool, error) {
	if r.CoreReconciler == nil {
		return false, fmt.Errorf("core reconciler not initialized")
	}

	tierStatus, err := r.CoreReconciler.Reconcile(ctx, deployment)
	if err != nil {
		deployment.Status.Core = tierStatus
		conditions.SetCoreCondition(deployment, tierStatus)
		return false, err
	}

	deployment.Status.Core = tierStatus
	conditions.SetCoreCondition(deployment, tierStatus)

	return tierStatus != nil && tierStatus.Ready, nil
}

// reconcileRest reconciles the rest tier
func (r *CarbideDeploymentReconciler) reconcileRest(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (bool, error) {
	if r.RestReconciler == nil {
		return false, fmt.Errorf("rest reconciler not initialized")
	}

	tierStatus, err := r.RestReconciler.Reconcile(ctx, deployment)
	if err != nil {
		deployment.Status.Rest = tierStatus
		conditions.SetRestCondition(deployment, tierStatus)
		return false, err
	}

	deployment.Status.Rest = tierStatus
	conditions.SetRestCondition(deployment, tierStatus)

	return tierStatus != nil && tierStatus.Ready, nil
}

// reconcileDelete handles deletion of CarbideDeployment
func (r *CarbideDeploymentReconciler) reconcileDelete(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling deletion")

	if !controllerutil.ContainsFinalizer(deployment, carbideFinalizer) {
		return ctrl.Result{}, nil
	}

	// Cleanup in reverse order: Rest -> Core -> Infrastructure
	// Resources are deleted automatically via owner references, but we can add
	// custom cleanup logic here if needed

	r.Recorder.Event(deployment, corev1.EventTypeNormal, "Deleting", "Cleaning up managed resources")
	logger.Info("Performing cleanup")

	// Remove finalizer
	controllerutil.RemoveFinalizer(deployment, carbideFinalizer)
	if err := r.Update(ctx, deployment); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Deletion complete")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *CarbideDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("carbidedeployment-controller")
	}

	// Initialize tier reconcilers
	r.InfrastructureReconciler = &InfrastructureReconciler{
		Client: r.Client,
		Scheme: r.Scheme,
	}
	r.CoreReconciler = &CoreReconciler{
		Client: r.Client,
		Scheme: r.Scheme,
	}
	r.RestReconciler = &RestReconciler{
		Client: r.Client,
		Scheme: r.Scheme,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&carbitev1alpha1.CarbideDeployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&batchv1.Job{}).
		Named("carbidedeployment").
		Complete(r)
}

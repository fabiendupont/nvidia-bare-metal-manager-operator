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
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources/infrastructure"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/utils"
)

// InfrastructureReconciler reconciles infrastructure tier components
type InfrastructureReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles the infrastructure tier
func (r *InfrastructureReconciler) Reconcile(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment) (*carbitev1alpha1.TierStatus, error) {
	logger := log.FromContext(ctx).WithValues("tier", "infrastructure")
	logger.Info("Reconciling infrastructure tier")

	infraConfig := deployment.Spec.Infrastructure
	if infraConfig == nil {
		return nil, fmt.Errorf("infrastructure config is nil")
	}

	// Determine namespace
	namespace := infraConfig.Namespace
	if namespace == "" {
		namespace = "carbide-operators"
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

	// Validate StorageClass if specified
	if infraConfig.StorageClass != "" {
		logger.Info("Validating StorageClass", "name", infraConfig.StorageClass)
		if err := utils.ValidateStorageClass(ctx, r.Client, infraConfig.StorageClass); err != nil {
			tierStatus.Message = fmt.Sprintf("StorageClass validation failed: %v", err)
			return tierStatus, err
		}
	}

	// Create PostgreSQL init ConfigMap (must exist before PostgresCluster CR)
	if deployment.Spec.Infrastructure.PostgreSQL.Mode != carbitev1alpha1.ExternalMode {
		initCM := infrastructure.BuildPostgreSQLInitConfigMap(deployment, namespace)
		if err := r.createOrUpdate(ctx, initCM); err != nil {
			tierStatus.Message = fmt.Sprintf("PostgreSQL init ConfigMap failed: %v", err)
			return tierStatus, err
		}
	}

	// Reconcile PostgreSQL
	pgReady, pgErr := r.reconcilePostgreSQL(ctx, deployment, namespace)
	tierStatus.Components = append(tierStatus.Components, carbitev1alpha1.ComponentStatus{
		Name:               "PostgreSQL",
		Ready:              pgReady,
		Message:            r.getComponentMessage("PostgreSQL", pgReady, pgErr),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	})
	if pgErr != nil {
		tierStatus.Message = fmt.Sprintf("PostgreSQL reconciliation failed: %v", pgErr)
		return tierStatus, pgErr
	}

	// Check overall tier readiness
	tierStatus.Ready = pgReady
	if tierStatus.Ready {
		tierStatus.Message = "All infrastructure components ready"
	} else {
		tierStatus.Message = "Waiting for infrastructure components to be ready"
	}

	logger.Info("Infrastructure tier reconciliation complete", "ready", tierStatus.Ready)
	return tierStatus, nil
}

// reconcilePostgreSQL reconciles PostgreSQL component
func (r *InfrastructureReconciler) reconcilePostgreSQL(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string) (bool, error) {
	logger := log.FromContext(ctx).WithValues("component", "postgresql")
	pgConfig := deployment.Spec.Infrastructure.PostgreSQL

	mode := pgConfig.Mode
	if mode == "" {
		mode = carbitev1alpha1.ManagedMode
	}

	switch mode {
	case carbitev1alpha1.ManagedMode:
		return r.reconcileManagedPostgreSQL(ctx, deployment, namespace, &pgConfig)
	case carbitev1alpha1.ExternalMode:
		return r.reconcileExternalPostgreSQL(ctx, deployment, namespace, &pgConfig)
	default:
		logger.Error(fmt.Errorf("unknown mode"), "Invalid PostgreSQL mode", "mode", mode)
		return false, fmt.Errorf("invalid PostgreSQL mode: %s", mode)
	}
}

// reconcileManagedPostgreSQL reconciles managed PostgreSQL
func (r *InfrastructureReconciler) reconcileManagedPostgreSQL(ctx context.Context, deployment *carbitev1alpha1.CarbideDeployment, namespace string, _ *carbitev1alpha1.PostgreSQLConfig) (bool, error) {
	logger := log.FromContext(ctx).WithValues("mode", "managed")
	logger.Info("Reconciling managed PostgreSQL")

	// Validate PostgreSQL operator is installed
	if err := utils.ValidatePostgresOperator(ctx, r.Client); err != nil {
		logger.Error(err, "PostgreSQL operator not found")
		return false, err
	}

	// Build PostgreSQL cluster CR
	pgCluster, err := infrastructure.BuildPostgreSQLCluster(deployment, namespace)
	if err != nil {
		logger.Error(err, "Failed to build PostgresCluster CR")
		return false, err
	}

	// Create or update PostgresCluster CR
	logger.Info("Creating or updating PostgresCluster CR", "name", pgCluster.GetName())
	if err := r.createOrUpdateUnstructured(ctx, pgCluster); err != nil {
		logger.Error(err, "Failed to create/update PostgresCluster CR")
		return false, err
	}

	// Check if PostgreSQL is ready
	ready, err := utils.IsPostgreSQLReady(ctx, r.Client, namespace, infrastructure.PostgreSQLClusterName)
	if err != nil {
		logger.Error(err, "Failed to check PostgreSQL readiness")
		return false, err
	}

	if ready {
		logger.Info("Managed PostgreSQL is ready")
	} else {
		logger.Info("Managed PostgreSQL is not ready yet")
	}

	return ready, nil
}

// reconcileExternalPostgreSQL validates external PostgreSQL connectivity
func (r *InfrastructureReconciler) reconcileExternalPostgreSQL(ctx context.Context, _ *carbitev1alpha1.CarbideDeployment, namespace string, config *carbitev1alpha1.PostgreSQLConfig) (bool, error) {
	logger := log.FromContext(ctx).WithValues("mode", "external")
	logger.Info("Validating external PostgreSQL")

	if config.Connection == nil {
		return false, fmt.Errorf("external PostgreSQL connection config is required")
	}

	// Validate connectivity to external PostgreSQL
	if err := utils.ValidateExternalPostgreSQL(ctx, r.Client, namespace, config.Connection); err != nil {
		logger.Error(err, "External PostgreSQL validation failed")
		return false, err
	}

	logger.Info("External PostgreSQL is accessible")
	return true, nil
}

// ensureNamespace ensures the namespace exists
func (r *InfrastructureReconciler) ensureNamespace(ctx context.Context, name string) error {
	logger := log.FromContext(ctx)

	ns := &corev1.Namespace{}
	ns.Name = name

	err := r.Get(ctx, client.ObjectKey{Name: name}, ns)
	if err == nil {
		// Namespace already exists
		return nil
	}

	if !errors.IsNotFound(err) {
		// Unexpected error
		return err
	}

	// Create namespace
	logger.Info("Creating namespace", "namespace", name)
	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "carbide-operator",
			},
		},
	}

	return r.Create(ctx, ns)
}

// getComponentMessage generates a status message for a component
func (r *InfrastructureReconciler) getComponentMessage(name string, ready bool, err error) string {
	if err != nil {
		return fmt.Sprintf("%s: %v", name, err)
	}
	if ready {
		return fmt.Sprintf("%s is ready", name)
	}
	return fmt.Sprintf("%s is not ready", name)
}

// failedStatus creates a failed tier status
func (r *InfrastructureReconciler) failedStatus(message string, err error) *carbitev1alpha1.TierStatus {
	return &carbitev1alpha1.TierStatus{
		Ready:              false,
		Message:            fmt.Sprintf("%s: %v", message, err),
		LastTransitionTime: &metav1.Time{Time: metav1.Now().Time},
	}
}

// createOrUpdate creates or updates a Kubernetes object
func (r *InfrastructureReconciler) createOrUpdate(ctx context.Context, obj client.Object) error {
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

// createOrUpdateUnstructured creates or updates an unstructured resource
func (r *InfrastructureReconciler) createOrUpdateUnstructured(ctx context.Context, obj *unstructured.Unstructured) error {
	logger := log.FromContext(ctx)

	// Try to get the existing resource
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())

	err := r.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, existing)

	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Resource doesn't exist, create it
			logger.Info("Creating resource", "gvk", obj.GroupVersionKind(), "name", obj.GetName())
			return r.Create(ctx, obj)
		}
		return err
	}

	// Resource exists, update it
	logger.Info("Updating resource", "gvk", obj.GroupVersionKind(), "name", obj.GetName())
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

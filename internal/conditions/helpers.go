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

package conditions

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

// SetCondition sets or updates a condition in the CarbideDeployment status
func SetCondition(deployment *carbitev1alpha1.CarbideDeployment, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: deployment.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// SetInfrastructureCondition sets the InfrastructureReady condition based on tier status
func SetInfrastructureCondition(deployment *carbitev1alpha1.CarbideDeployment, tierStatus *carbitev1alpha1.TierStatus) {
	if tierStatus == nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeInfrastructureReady, metav1.ConditionFalse, "NotConfigured", "Infrastructure tier not configured")
		return
	}

	if tierStatus.Ready {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeInfrastructureReady, metav1.ConditionTrue, "Ready", tierStatus.Message)
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeInfrastructureReady, metav1.ConditionFalse, "NotReady", tierStatus.Message)
	}
}

// SetCoreCondition sets the CoreReady condition based on tier status
func SetCoreCondition(deployment *carbitev1alpha1.CarbideDeployment, tierStatus *carbitev1alpha1.TierStatus) {
	if tierStatus == nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeCoreReady, metav1.ConditionFalse, "NotConfigured", "Core tier not configured")
		return
	}

	if tierStatus.Ready {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeCoreReady, metav1.ConditionTrue, "Ready", tierStatus.Message)
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeCoreReady, metav1.ConditionFalse, "NotReady", tierStatus.Message)
	}
}

// SetRestCondition sets the RestReady condition based on tier status
func SetRestCondition(deployment *carbitev1alpha1.CarbideDeployment, tierStatus *carbitev1alpha1.TierStatus) {
	if tierStatus == nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRestReady, metav1.ConditionFalse, "NotConfigured", "REST tier not configured")
		return
	}

	if tierStatus.Ready {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRestReady, metav1.ConditionTrue, "Ready", tierStatus.Message)
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRestReady, metav1.ConditionFalse, "NotReady", tierStatus.Message)
	}
}

// SetTLSCondition sets the TLSReady condition
func SetTLSCondition(deployment *carbitev1alpha1.CarbideDeployment, available bool, message string) {
	if available {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeTLSReady, metav1.ConditionTrue, "Available", message)
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeTLSReady, metav1.ConditionFalse, "Unavailable", message)
	}
}

// SetSPIFFEAvailableCondition sets the SPIFFEAvailable condition
func SetSPIFFEAvailableCondition(deployment *carbitev1alpha1.CarbideDeployment, available bool) {
	if available {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeSPIFFEAvailable, metav1.ConditionTrue, "Detected", "SPIRE CSI driver detected")
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeSPIFFEAvailable, metav1.ConditionFalse, "NotDetected", "SPIRE CSI driver not found")
	}
}

// SetCertManagerAvailableCondition sets the CertManagerAvailable condition
func SetCertManagerAvailableCondition(deployment *carbitev1alpha1.CarbideDeployment, available bool) {
	if available {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeCertManagerAvailable, metav1.ConditionTrue, "Detected", "cert-manager CRDs detected")
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeCertManagerAvailable, metav1.ConditionFalse, "NotDetected", "cert-manager CRDs not found")
	}
}

// SetVaultCondition sets the VaultReady condition
func SetVaultCondition(deployment *carbitev1alpha1.CarbideDeployment, tierStatus *carbitev1alpha1.TierStatus) {
	if tierStatus == nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeVaultReady, metav1.ConditionFalse, "NotConfigured", "Vault not configured")
		return
	}
	if tierStatus.Ready {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeVaultReady, metav1.ConditionTrue, "Ready", tierStatus.Message)
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeVaultReady, metav1.ConditionFalse, "NotReady", tierStatus.Message)
	}
}

// SetRLACondition sets the RLAReady condition
func SetRLACondition(deployment *carbitev1alpha1.CarbideDeployment, tierStatus *carbitev1alpha1.TierStatus) {
	if tierStatus == nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRLAReady, metav1.ConditionFalse, "NotConfigured", "RLA not configured")
		return
	}
	if tierStatus.Ready {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRLAReady, metav1.ConditionTrue, "Ready", tierStatus.Message)
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRLAReady, metav1.ConditionFalse, "NotReady", tierStatus.Message)
	}
}

// SetPSMCondition sets the PSMReady condition
func SetPSMCondition(deployment *carbitev1alpha1.CarbideDeployment, tierStatus *carbitev1alpha1.TierStatus) {
	if tierStatus == nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypePSMReady, metav1.ConditionFalse, "NotConfigured", "PSM not configured")
		return
	}
	if tierStatus.Ready {
		SetCondition(deployment, carbitev1alpha1.ConditionTypePSMReady, metav1.ConditionTrue, "Ready", tierStatus.Message)
	} else {
		SetCondition(deployment, carbitev1alpha1.ConditionTypePSMReady, metav1.ConditionFalse, "NotReady", tierStatus.Message)
	}
}

// SetReadyCondition sets the overall Ready condition based on all tier statuses
func SetReadyCondition(deployment *carbitev1alpha1.CarbideDeployment) {
	// Check TLS readiness
	tlsReady := IsConditionTrue(deployment, carbitev1alpha1.ConditionTypeTLSReady)
	if deployment.Spec.TLS != nil && !tlsReady {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeReady, metav1.ConditionFalse, "NotReady", "TLS backend not available")
		deployment.Status.Phase = carbitev1alpha1.PhaseFailed
		return
	}

	// Check if all required tiers are ready
	infraReady := deployment.Spec.Infrastructure == nil ||
		(deployment.Status.Infrastructure != nil && deployment.Status.Infrastructure.Ready)
	coreReady := deployment.Status.Core != nil && deployment.Status.Core.Ready
	restReady := deployment.Spec.Rest == nil ||
		(deployment.Status.Rest != nil && deployment.Status.Rest.Ready)

	if infraReady && coreReady && restReady {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeReady, metav1.ConditionTrue, "Ready", "All components are ready")
		deployment.Status.Phase = carbitev1alpha1.PhaseReady
	} else {
		var message string
		if !infraReady {
			message = "Infrastructure tier not ready"
		} else if !coreReady {
			message = "Core tier not ready"
		} else if !restReady {
			message = "REST tier not ready"
		}
		SetCondition(deployment, carbitev1alpha1.ConditionTypeReady, metav1.ConditionFalse, "NotReady", message)
		deployment.Status.Phase = carbitev1alpha1.PhaseProvisioning
	}
}

// IsConditionTrue checks if a condition is present and has status True
func IsConditionTrue(deployment *carbitev1alpha1.CarbideDeployment, conditionType string) bool {
	condition := meta.FindStatusCondition(deployment.Status.Conditions, conditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

// GetCondition retrieves a condition by type
func GetCondition(deployment *carbitev1alpha1.CarbideDeployment, conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(deployment.Status.Conditions, conditionType)
}

// InitializeConditions initializes all conditions to Unknown status
func InitializeConditions(deployment *carbitev1alpha1.CarbideDeployment) {
	SetCondition(deployment, carbitev1alpha1.ConditionTypeReady, metav1.ConditionUnknown, "Initializing", "Starting deployment")

	if deployment.Spec.Infrastructure != nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeInfrastructureReady, metav1.ConditionUnknown, "Initializing", "Infrastructure tier initializing")
	}

	SetCondition(deployment, carbitev1alpha1.ConditionTypeCoreReady, metav1.ConditionUnknown, "Initializing", "Core tier initializing")

	if deployment.Spec.Rest != nil && deployment.Spec.Rest.Enabled {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRestReady, metav1.ConditionUnknown, "Initializing", "REST tier initializing")
	}

	// TLS conditions
	if deployment.Spec.TLS != nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeTLSReady, metav1.ConditionUnknown, "Initializing", "Checking TLS backend")
		switch deployment.Spec.TLS.Mode {
		case carbitev1alpha1.TLSModeSpiffe:
			SetCondition(deployment, carbitev1alpha1.ConditionTypeSPIFFEAvailable, metav1.ConditionUnknown, "Initializing", "Checking SPIRE availability")
		case carbitev1alpha1.TLSModeCertManager:
			SetCondition(deployment, carbitev1alpha1.ConditionTypeCertManagerAvailable, metav1.ConditionUnknown, "Initializing", "Checking cert-manager availability")
		}
	}

	// Vault condition
	if deployment.Spec.Core.Vault != nil {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeVaultReady, metav1.ConditionUnknown, "Initializing", "Vault initializing")
	}

	// RLA condition
	if deployment.Spec.Core.RLA != nil && deployment.Spec.Core.RLA.Enabled {
		SetCondition(deployment, carbitev1alpha1.ConditionTypeRLAReady, metav1.ConditionUnknown, "Initializing", "RLA initializing")
	}

	// PSM condition
	if deployment.Spec.Core.PSM != nil && deployment.Spec.Core.PSM.Enabled {
		SetCondition(deployment, carbitev1alpha1.ConditionTypePSMReady, metav1.ConditionUnknown, "Initializing", "PSM initializing")
	}

	deployment.Status.Phase = carbitev1alpha1.PhasePending
}

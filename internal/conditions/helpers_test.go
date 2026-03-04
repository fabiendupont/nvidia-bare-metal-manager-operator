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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

func newDeployment() *carbitev1alpha1.CarbideDeployment {
	return &carbitev1alpha1.CarbideDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: carbitev1alpha1.CarbideDeploymentSpec{
			Profile: carbitev1alpha1.ProfileSite,
			Version: "latest",
			Network: carbitev1alpha1.NetworkConfig{Domain: "carbide.local"},
			Core:    carbitev1alpha1.CoreConfig{},
		},
	}
}

func TestSetCondition(t *testing.T) {
	t.Run("sets a new condition", func(t *testing.T) {
		d := newDeployment()
		SetCondition(d, "TestType", metav1.ConditionTrue, "TestReason", "test message")

		if len(d.Status.Conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(d.Status.Conditions))
		}
		c := d.Status.Conditions[0]
		if c.Type != "TestType" {
			t.Errorf("expected type TestType, got %s", c.Type)
		}
		if c.Status != metav1.ConditionTrue {
			t.Errorf("expected status True, got %s", c.Status)
		}
		if c.Reason != "TestReason" {
			t.Errorf("expected reason TestReason, got %s", c.Reason)
		}
		if c.Message != "test message" {
			t.Errorf("expected message 'test message', got %s", c.Message)
		}
		if c.ObservedGeneration != 1 {
			t.Errorf("expected observedGeneration 1, got %d", c.ObservedGeneration)
		}
	})

	t.Run("updates an existing condition", func(t *testing.T) {
		d := newDeployment()
		SetCondition(d, "TestType", metav1.ConditionFalse, "OldReason", "old message")
		SetCondition(d, "TestType", metav1.ConditionTrue, "NewReason", "new message")

		if len(d.Status.Conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(d.Status.Conditions))
		}
		c := d.Status.Conditions[0]
		if c.Status != metav1.ConditionTrue {
			t.Errorf("expected status True, got %s", c.Status)
		}
		if c.Reason != "NewReason" {
			t.Errorf("expected reason NewReason, got %s", c.Reason)
		}
		if c.Message != "new message" {
			t.Errorf("expected message 'new message', got %s", c.Message)
		}
	})
}

func TestSetInfrastructureCondition(t *testing.T) {
	tests := []struct {
		name           string
		tierStatus     *carbitev1alpha1.TierStatus
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "nil tierStatus",
			tierStatus:     nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotConfigured",
		},
		{
			name:           "ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: true, Message: "all good"},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "Ready",
		},
		{
			name:           "not-ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: false, Message: "still starting"},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetInfrastructureCondition(d, tt.tierStatus)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeInfrastructureReady)
			if c == nil {
				t.Fatal("expected InfrastructureReady condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
		})
	}
}

func TestSetCoreCondition(t *testing.T) {
	tests := []struct {
		name           string
		tierStatus     *carbitev1alpha1.TierStatus
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "nil tierStatus",
			tierStatus:     nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotConfigured",
		},
		{
			name:           "ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: true, Message: "core ready"},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "Ready",
		},
		{
			name:           "not-ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: false, Message: "core pending"},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetCoreCondition(d, tt.tierStatus)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeCoreReady)
			if c == nil {
				t.Fatal("expected CoreReady condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
		})
	}
}

func TestSetRestCondition(t *testing.T) {
	tests := []struct {
		name           string
		tierStatus     *carbitev1alpha1.TierStatus
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "nil tierStatus",
			tierStatus:     nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotConfigured",
		},
		{
			name:           "ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: true, Message: "rest ready"},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "Ready",
		},
		{
			name:           "not-ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: false, Message: "rest pending"},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetRestCondition(d, tt.tierStatus)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeRestReady)
			if c == nil {
				t.Fatal("expected RestReady condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
		})
	}
}

func TestSetTLSCondition(t *testing.T) {
	tests := []struct {
		name           string
		available      bool
		message        string
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "available true",
			available:      true,
			message:        "TLS configured",
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "Available",
		},
		{
			name:           "available false",
			available:      false,
			message:        "TLS not configured",
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "Unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetTLSCondition(d, tt.available, tt.message)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeTLSReady)
			if c == nil {
				t.Fatal("expected TLSReady condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
			if c.Message != tt.message {
				t.Errorf("expected message %q, got %q", tt.message, c.Message)
			}
		})
	}
}

func TestSetSPIFFEAvailableCondition(t *testing.T) {
	tests := []struct {
		name            string
		available       bool
		expectedStatus  metav1.ConditionStatus
		expectedReason  string
		expectedMessage string
	}{
		{
			name:            "detected",
			available:       true,
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  "Detected",
			expectedMessage: "SPIRE CSI driver detected",
		},
		{
			name:            "not detected",
			available:       false,
			expectedStatus:  metav1.ConditionFalse,
			expectedReason:  "NotDetected",
			expectedMessage: "SPIRE CSI driver not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetSPIFFEAvailableCondition(d, tt.available)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeSPIFFEAvailable)
			if c == nil {
				t.Fatal("expected SPIFFEAvailable condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
			if c.Message != tt.expectedMessage {
				t.Errorf("expected message %q, got %q", tt.expectedMessage, c.Message)
			}
		})
	}
}

func TestSetCertManagerAvailableCondition(t *testing.T) {
	tests := []struct {
		name            string
		available       bool
		expectedStatus  metav1.ConditionStatus
		expectedReason  string
		expectedMessage string
	}{
		{
			name:            "detected",
			available:       true,
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  "Detected",
			expectedMessage: "cert-manager CRDs detected",
		},
		{
			name:            "not detected",
			available:       false,
			expectedStatus:  metav1.ConditionFalse,
			expectedReason:  "NotDetected",
			expectedMessage: "cert-manager CRDs not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetCertManagerAvailableCondition(d, tt.available)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeCertManagerAvailable)
			if c == nil {
				t.Fatal("expected CertManagerAvailable condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
			if c.Message != tt.expectedMessage {
				t.Errorf("expected message %q, got %q", tt.expectedMessage, c.Message)
			}
		})
	}
}

func TestSetVaultCondition(t *testing.T) {
	tests := []struct {
		name           string
		tierStatus     *carbitev1alpha1.TierStatus
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "nil tierStatus",
			tierStatus:     nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotConfigured",
		},
		{
			name:           "ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: true, Message: "vault ready"},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "Ready",
		},
		{
			name:           "not-ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: false, Message: "vault pending"},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetVaultCondition(d, tt.tierStatus)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeVaultReady)
			if c == nil {
				t.Fatal("expected VaultReady condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
		})
	}
}

func TestSetRLACondition(t *testing.T) {
	tests := []struct {
		name           string
		tierStatus     *carbitev1alpha1.TierStatus
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "nil tierStatus",
			tierStatus:     nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotConfigured",
		},
		{
			name:           "ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: true, Message: "rla ready"},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "Ready",
		},
		{
			name:           "not-ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: false, Message: "rla pending"},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetRLACondition(d, tt.tierStatus)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeRLAReady)
			if c == nil {
				t.Fatal("expected RLAReady condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
		})
	}
}

func TestSetPSMCondition(t *testing.T) {
	tests := []struct {
		name           string
		tierStatus     *carbitev1alpha1.TierStatus
		expectedStatus metav1.ConditionStatus
		expectedReason string
	}{
		{
			name:           "nil tierStatus",
			tierStatus:     nil,
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotConfigured",
		},
		{
			name:           "ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: true, Message: "psm ready"},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: "Ready",
		},
		{
			name:           "not-ready tierStatus",
			tierStatus:     &carbitev1alpha1.TierStatus{Ready: false, Message: "psm pending"},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: "NotReady",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			SetPSMCondition(d, tt.tierStatus)

			c := GetCondition(d, carbitev1alpha1.ConditionTypePSMReady)
			if c == nil {
				t.Fatal("expected PSMReady condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, c.Reason)
			}
		})
	}
}

func TestSetReadyCondition(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(d *carbitev1alpha1.CarbideDeployment)
		expectedStatus  metav1.ConditionStatus
		expectedPhase   carbitev1alpha1.DeploymentPhase
		expectedMessage string
	}{
		{
			name: "all tiers ready, no infrastructure spec",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Infrastructure = nil
				d.Status.Core = &carbitev1alpha1.TierStatus{Ready: true, Message: "ok"}
				d.Spec.Rest = nil
			},
			expectedStatus:  metav1.ConditionTrue,
			expectedPhase:   carbitev1alpha1.PhaseReady,
			expectedMessage: "All components are ready",
		},
		{
			name: "all tiers ready with infrastructure and rest",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{}
				d.Status.Infrastructure = &carbitev1alpha1.TierStatus{Ready: true}
				d.Status.Core = &carbitev1alpha1.TierStatus{Ready: true}
				d.Spec.Rest = &carbitev1alpha1.RestConfig{Enabled: true}
				d.Status.Rest = &carbitev1alpha1.TierStatus{Ready: true}
			},
			expectedStatus:  metav1.ConditionTrue,
			expectedPhase:   carbitev1alpha1.PhaseReady,
			expectedMessage: "All components are ready",
		},
		{
			name: "infrastructure not ready",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{}
				d.Status.Infrastructure = &carbitev1alpha1.TierStatus{Ready: false}
				d.Status.Core = &carbitev1alpha1.TierStatus{Ready: true}
				d.Spec.Rest = nil
			},
			expectedStatus:  metav1.ConditionFalse,
			expectedPhase:   carbitev1alpha1.PhaseProvisioning,
			expectedMessage: "Infrastructure tier not ready",
		},
		{
			name: "core not ready",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Infrastructure = nil
				d.Status.Core = &carbitev1alpha1.TierStatus{Ready: false}
				d.Spec.Rest = nil
			},
			expectedStatus:  metav1.ConditionFalse,
			expectedPhase:   carbitev1alpha1.PhaseProvisioning,
			expectedMessage: "Core tier not ready",
		},
		{
			name: "rest not ready",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.Infrastructure = nil
				d.Status.Core = &carbitev1alpha1.TierStatus{Ready: true}
				d.Spec.Rest = &carbitev1alpha1.RestConfig{Enabled: true}
				d.Status.Rest = &carbitev1alpha1.TierStatus{Ready: false}
			},
			expectedStatus:  metav1.ConditionFalse,
			expectedPhase:   carbitev1alpha1.PhaseProvisioning,
			expectedMessage: "REST tier not ready",
		},
		{
			name: "TLS not ready",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				d.Spec.TLS = &carbitev1alpha1.TLSConfig{Mode: carbitev1alpha1.TLSModeSpiffe}
				// TLSReady condition not set, so IsConditionTrue returns false
				d.Status.Core = &carbitev1alpha1.TierStatus{Ready: true}
			},
			expectedStatus:  metav1.ConditionFalse,
			expectedPhase:   carbitev1alpha1.PhaseFailed,
			expectedMessage: "TLS backend not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			tt.setup(d)
			SetReadyCondition(d)

			c := GetCondition(d, carbitev1alpha1.ConditionTypeReady)
			if c == nil {
				t.Fatal("expected Ready condition to be set")
			}
			if c.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, c.Status)
			}
			if c.Message != tt.expectedMessage {
				t.Errorf("expected message %q, got %q", tt.expectedMessage, c.Message)
			}
			if d.Status.Phase != tt.expectedPhase {
				t.Errorf("expected phase %s, got %s", tt.expectedPhase, d.Status.Phase)
			}
		})
	}
}

func TestIsConditionTrue(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(d *carbitev1alpha1.CarbideDeployment)
		condType string
		expected bool
	}{
		{
			name: "condition present and true",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				SetCondition(d, "TestCond", metav1.ConditionTrue, "R", "m")
			},
			condType: "TestCond",
			expected: true,
		},
		{
			name: "condition present and false",
			setup: func(d *carbitev1alpha1.CarbideDeployment) {
				SetCondition(d, "TestCond", metav1.ConditionFalse, "R", "m")
			},
			condType: "TestCond",
			expected: false,
		},
		{
			name:     "condition not present",
			setup:    func(d *carbitev1alpha1.CarbideDeployment) {},
			condType: "TestCond",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDeployment()
			tt.setup(d)
			result := IsConditionTrue(d, tt.condType)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGetCondition(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		d := newDeployment()
		SetCondition(d, "TestCond", metav1.ConditionTrue, "R", "m")

		c := GetCondition(d, "TestCond")
		if c == nil {
			t.Fatal("expected condition to be found")
		}
		if c.Type != "TestCond" {
			t.Errorf("expected type TestCond, got %s", c.Type)
		}
	})

	t.Run("not found", func(t *testing.T) {
		d := newDeployment()

		c := GetCondition(d, "NonExistent")
		if c != nil {
			t.Errorf("expected nil, got %v", c)
		}
	})
}

func TestInitializeConditions(t *testing.T) {
	t.Run("management profile", func(t *testing.T) {
		d := newDeployment()
		d.Spec.Profile = carbitev1alpha1.ProfileManagement
		d.Spec.Infrastructure = &carbitev1alpha1.InfrastructureConfig{}
		d.Spec.Rest = &carbitev1alpha1.RestConfig{Enabled: true}

		InitializeConditions(d)

		if d.Status.Phase != carbitev1alpha1.PhasePending {
			t.Errorf("expected phase Pending, got %s", d.Status.Phase)
		}

		expectedConditions := []string{
			carbitev1alpha1.ConditionTypeReady,
			carbitev1alpha1.ConditionTypeInfrastructureReady,
			carbitev1alpha1.ConditionTypeCoreReady,
			carbitev1alpha1.ConditionTypeRestReady,
		}
		for _, ct := range expectedConditions {
			c := GetCondition(d, ct)
			if c == nil {
				t.Errorf("expected condition %s to be initialized", ct)
				continue
			}
			if c.Status != metav1.ConditionUnknown {
				t.Errorf("expected condition %s to have status Unknown, got %s", ct, c.Status)
			}
			if c.Reason != "Initializing" {
				t.Errorf("expected condition %s to have reason Initializing, got %s", ct, c.Reason)
			}
		}
	})

	t.Run("site profile with RLA, PSM, and Vault", func(t *testing.T) {
		d := newDeployment()
		d.Spec.Profile = carbitev1alpha1.ProfileSite
		d.Spec.Infrastructure = nil
		d.Spec.Rest = nil
		d.Spec.Core.Vault = &carbitev1alpha1.VaultConfig{}
		d.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: true}
		d.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: true}

		InitializeConditions(d)

		if d.Status.Phase != carbitev1alpha1.PhasePending {
			t.Errorf("expected phase Pending, got %s", d.Status.Phase)
		}

		// Should have Ready and CoreReady
		for _, ct := range []string{
			carbitev1alpha1.ConditionTypeReady,
			carbitev1alpha1.ConditionTypeCoreReady,
			carbitev1alpha1.ConditionTypeVaultReady,
			carbitev1alpha1.ConditionTypeRLAReady,
			carbitev1alpha1.ConditionTypePSMReady,
		} {
			c := GetCondition(d, ct)
			if c == nil {
				t.Errorf("expected condition %s to be initialized", ct)
				continue
			}
			if c.Status != metav1.ConditionUnknown {
				t.Errorf("expected condition %s to have status Unknown, got %s", ct, c.Status)
			}
		}

		// Should NOT have InfrastructureReady or RestReady
		for _, ct := range []string{
			carbitev1alpha1.ConditionTypeInfrastructureReady,
			carbitev1alpha1.ConditionTypeRestReady,
		} {
			c := GetCondition(d, ct)
			if c != nil {
				t.Errorf("expected condition %s to not be set, but it was", ct)
			}
		}
	})

	t.Run("TLS mode spiffe", func(t *testing.T) {
		d := newDeployment()
		d.Spec.TLS = &carbitev1alpha1.TLSConfig{Mode: carbitev1alpha1.TLSModeSpiffe}

		InitializeConditions(d)

		c := GetCondition(d, carbitev1alpha1.ConditionTypeTLSReady)
		if c == nil {
			t.Fatal("expected TLSReady condition to be initialized")
		}
		if c.Status != metav1.ConditionUnknown {
			t.Errorf("expected status Unknown, got %s", c.Status)
		}

		c = GetCondition(d, carbitev1alpha1.ConditionTypeSPIFFEAvailable)
		if c == nil {
			t.Fatal("expected SPIFFEAvailable condition to be initialized")
		}
		if c.Status != metav1.ConditionUnknown {
			t.Errorf("expected status Unknown, got %s", c.Status)
		}

		c = GetCondition(d, carbitev1alpha1.ConditionTypeCertManagerAvailable)
		if c != nil {
			t.Error("expected CertManagerAvailable condition to not be set for spiffe mode")
		}
	})

	t.Run("TLS mode certManager", func(t *testing.T) {
		d := newDeployment()
		d.Spec.TLS = &carbitev1alpha1.TLSConfig{Mode: carbitev1alpha1.TLSModeCertManager}

		InitializeConditions(d)

		c := GetCondition(d, carbitev1alpha1.ConditionTypeTLSReady)
		if c == nil {
			t.Fatal("expected TLSReady condition to be initialized")
		}

		c = GetCondition(d, carbitev1alpha1.ConditionTypeCertManagerAvailable)
		if c == nil {
			t.Fatal("expected CertManagerAvailable condition to be initialized")
		}
		if c.Status != metav1.ConditionUnknown {
			t.Errorf("expected status Unknown, got %s", c.Status)
		}

		c = GetCondition(d, carbitev1alpha1.ConditionTypeSPIFFEAvailable)
		if c != nil {
			t.Error("expected SPIFFEAvailable condition to not be set for certManager mode")
		}
	})

	t.Run("no TLS configured", func(t *testing.T) {
		d := newDeployment()
		d.Spec.TLS = nil

		InitializeConditions(d)

		c := GetCondition(d, carbitev1alpha1.ConditionTypeTLSReady)
		if c != nil {
			t.Error("expected TLSReady condition to not be set when TLS is nil")
		}
	})

	t.Run("RLA disabled not initialized", func(t *testing.T) {
		d := newDeployment()
		d.Spec.Core.RLA = &carbitev1alpha1.RLAConfig{Enabled: false}

		InitializeConditions(d)

		c := GetCondition(d, carbitev1alpha1.ConditionTypeRLAReady)
		if c != nil {
			t.Error("expected RLAReady condition to not be set when RLA is disabled")
		}
	})

	t.Run("PSM disabled not initialized", func(t *testing.T) {
		d := newDeployment()
		d.Spec.Core.PSM = &carbitev1alpha1.PSMConfig{Enabled: false}

		InitializeConditions(d)

		c := GetCondition(d, carbitev1alpha1.ConditionTypePSMReady)
		if c != nil {
			t.Error("expected PSMReady condition to not be set when PSM is disabled")
		}
	})

	t.Run("Rest disabled not initialized", func(t *testing.T) {
		d := newDeployment()
		d.Spec.Rest = &carbitev1alpha1.RestConfig{Enabled: false}

		InitializeConditions(d)

		c := GetCondition(d, carbitev1alpha1.ConditionTypeRestReady)
		if c != nil {
			t.Error("expected RestReady condition to not be set when Rest is disabled")
		}
	})
}

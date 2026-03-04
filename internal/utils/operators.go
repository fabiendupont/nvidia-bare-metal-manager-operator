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

package utils

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateOperatorInstalled checks if a required operator is installed by verifying its CRD exists
func ValidateOperatorInstalled(ctx context.Context, c client.Client, crdName string) error {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := c.Get(ctx, types.NamespacedName{Name: crdName}, crd); err != nil {
		return fmt.Errorf("operator CRD %s not found: %w (operator may not be installed)", crdName, err)
	}
	return nil
}

// ValidatePostgresOperator checks if the Crunchy PostgreSQL operator is installed
func ValidatePostgresOperator(ctx context.Context, c client.Client) error {
	if err := ValidateOperatorInstalled(ctx, c, "postgresclusters.postgres-operator.crunchydata.com"); err != nil {
		return fmt.Errorf("Crunchy PostgreSQL operator not installed: %w\n"+
			"Install from: https://access.crunchydata.com/documentation/postgres-operator/latest/installation/", err)
	}
	return nil
}

// ValidateKeycloakOperator checks if the Red Hat Build of Keycloak (RHBK) operator is installed
func ValidateKeycloakOperator(ctx context.Context, c client.Client) error {
	// Check for the Keycloak CRD first
	if err := ValidateOperatorInstalled(ctx, c, "keycloaks.k8s.keycloak.org"); err != nil {
		return fmt.Errorf("Red Hat Build of Keycloak operator not installed: %w\n"+
			"Install from OperatorHub or: https://www.keycloak.org/operator/installation", err)
	}

	// Also check for KeycloakRealmImport CRD
	if err := ValidateOperatorInstalled(ctx, c, "keycloakrealmimports.k8s.keycloak.org"); err != nil {
		return fmt.Errorf("Keycloak operator CRD keycloakrealmimports not found: %w", err)
	}

	return nil
}

// IsCRDAvailable checks if a CRD exists in the cluster
func IsCRDAvailable(ctx context.Context, c client.Client, crdName string) (bool, error) {
	err := ValidateOperatorInstalled(ctx, c, crdName)
	if err != nil {
		return false, nil
	}
	return true, nil
}

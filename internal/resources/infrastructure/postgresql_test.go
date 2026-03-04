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

package infrastructure

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

func newTestDeployment(databases []string) *carbitev1alpha1.CarbideDeployment {
	return &carbitev1alpha1.CarbideDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
		Spec: carbitev1alpha1.CarbideDeploymentSpec{
			Profile: carbitev1alpha1.ProfileManagementWithSite,
			Version: "latest",
			Network: carbitev1alpha1.NetworkConfig{
				Domain: "carbide.local",
			},
			Infrastructure: &carbitev1alpha1.InfrastructureConfig{
				PostgreSQL: carbitev1alpha1.PostgreSQLConfig{
					Mode:      carbitev1alpha1.ManagedMode,
					Databases: databases,
				},
			},
			Core: carbitev1alpha1.CoreConfig{},
		},
	}
}

func TestBuildPostgreSQLCluster(t *testing.T) {
	deployment := newTestDeployment([]string{"carbide", "forge"})
	deployment.Spec.Infrastructure.PostgreSQL.Version = "16"
	deployment.Spec.Infrastructure.PostgreSQL.Replicas = 3

	cluster, err := BuildPostgreSQLCluster(deployment, "test-ns")
	if err != nil {
		t.Fatalf("BuildPostgreSQLCluster returned error: %v", err)
	}

	// Verify name
	if cluster.GetName() != "carbide-postgres" {
		t.Errorf("expected name %q, got %q", "carbide-postgres", cluster.GetName())
	}

	// Verify namespace
	if cluster.GetNamespace() != "test-ns" {
		t.Errorf("expected namespace %q, got %q", "test-ns", cluster.GetNamespace())
	}

	// Verify kind and apiVersion
	if cluster.GetKind() != "PostgresCluster" {
		t.Errorf("expected kind %q, got %q", "PostgresCluster", cluster.GetKind())
	}
	if cluster.GetAPIVersion() != "postgres-operator.crunchydata.com/v1beta1" {
		t.Errorf("expected apiVersion %q, got %q", "postgres-operator.crunchydata.com/v1beta1", cluster.GetAPIVersion())
	}

	spec := cluster.Object["spec"].(map[string]interface{})

	// Verify version
	if spec["postgresVersion"] != "16" {
		t.Errorf("expected postgresVersion %q, got %v", "16", spec["postgresVersion"])
	}

	// Verify replicas
	instances := spec["instances"].([]interface{})
	instance := instances[0].(map[string]interface{})
	if instance["replicas"] != int32(3) {
		t.Errorf("expected replicas 3, got %v", instance["replicas"])
	}

	// Verify storage default
	dataVolume := instance["dataVolumeClaimSpec"].(map[string]interface{})
	resources := dataVolume["resources"].(map[string]interface{})
	requests := resources["requests"].(map[string]interface{})
	if requests["storage"] != "10Gi" {
		t.Errorf("expected storage %q, got %v", "10Gi", requests["storage"])
	}

	// Verify databaseInitSQL references the init ConfigMap
	initSQL := spec["databaseInitSQL"].(map[string]interface{})
	if initSQL["name"] != PostgreSQLInitConfigMapName {
		t.Errorf("expected databaseInitSQL name %q, got %v", PostgreSQLInitConfigMapName, initSQL["name"])
	}
}

func TestBuildPostgreSQLCluster_DefaultValues(t *testing.T) {
	deployment := newTestDeployment(nil)
	// Leave version and replicas at zero values to test defaults

	cluster, err := BuildPostgreSQLCluster(deployment, "test-ns")
	if err != nil {
		t.Fatalf("BuildPostgreSQLCluster returned error: %v", err)
	}

	spec := cluster.Object["spec"].(map[string]interface{})

	if spec["postgresVersion"] != "16" {
		t.Errorf("expected default postgresVersion %q, got %v", "16", spec["postgresVersion"])
	}

	instances := spec["instances"].([]interface{})
	instance := instances[0].(map[string]interface{})
	if instance["replicas"] != int32(1) {
		t.Errorf("expected default replicas 1, got %v", instance["replicas"])
	}
}

func TestBuildPostgreSQLUsers_SiteDatabases(t *testing.T) {
	// Site databases: carbide, forge, rla, psm => 4 separate users
	deployment := newTestDeployment([]string{"carbide", "forge", "rla", "psm"})

	cluster, err := BuildPostgreSQLCluster(deployment, "test-ns")
	if err != nil {
		t.Fatalf("BuildPostgreSQLCluster returned error: %v", err)
	}

	spec := cluster.Object["spec"].(map[string]interface{})
	users := spec["users"].([]interface{})

	if len(users) != 4 {
		t.Fatalf("expected 4 users for site databases, got %d", len(users))
	}

	// Build a map of user -> databases for verification
	userDBs := make(map[string][]string)
	for _, u := range users {
		user := u.(map[string]interface{})
		name := user["name"].(string)
		dbs := user["databases"].([]interface{})
		for _, db := range dbs {
			userDBs[name] = append(userDBs[name], db.(string))
		}
	}

	// Each site database should have its own user with a single database
	for _, expected := range []string{"carbide", "forge", "rla", "psm"} {
		dbs, ok := userDBs[expected]
		if !ok {
			t.Errorf("expected user %q, not found", expected)
			continue
		}
		if len(dbs) != 1 || dbs[0] != expected {
			t.Errorf("expected user %q to own database [%q], got %v", expected, expected, dbs)
		}
	}
}

func TestBuildPostgreSQLUsers_ManagementDatabases(t *testing.T) {
	// Management databases: forge, temporal, temporal_visibility, keycloak
	// temporal user should get 2 databases (temporal, temporal_visibility)
	deployment := newTestDeployment([]string{"forge", "temporal", "temporal_visibility", "keycloak"})

	cluster, err := BuildPostgreSQLCluster(deployment, "test-ns")
	if err != nil {
		t.Fatalf("BuildPostgreSQLCluster returned error: %v", err)
	}

	spec := cluster.Object["spec"].(map[string]interface{})
	users := spec["users"].([]interface{})

	userDBs := make(map[string][]string)
	for _, u := range users {
		user := u.(map[string]interface{})
		name := user["name"].(string)
		dbs := user["databases"].([]interface{})
		for _, db := range dbs {
			userDBs[name] = append(userDBs[name], db.(string))
		}
	}

	// forge user -> forge database
	if dbs, ok := userDBs["forge"]; !ok {
		t.Error("expected user 'forge', not found")
	} else if len(dbs) != 1 || dbs[0] != "forge" {
		t.Errorf("expected forge user to own [forge], got %v", dbs)
	}

	// temporal user -> temporal and temporal_visibility
	if dbs, ok := userDBs["temporal"]; !ok {
		t.Error("expected user 'temporal', not found")
	} else if len(dbs) != 2 {
		t.Errorf("expected temporal user to own 2 databases, got %v", dbs)
	} else {
		hasT, hasTV := false, false
		for _, db := range dbs {
			if db == "temporal" {
				hasT = true
			}
			if db == "temporal_visibility" {
				hasTV = true
			}
		}
		if !hasT || !hasTV {
			t.Errorf("expected temporal user to own [temporal, temporal_visibility], got %v", dbs)
		}
	}

	// keycloak user -> keycloak database
	if dbs, ok := userDBs["keycloak"]; !ok {
		t.Error("expected user 'keycloak', not found")
	} else if len(dbs) != 1 || dbs[0] != "keycloak" {
		t.Errorf("expected keycloak user to own [keycloak], got %v", dbs)
	}

	// Total unique users: forge, temporal, keycloak = 3
	if len(users) != 3 {
		t.Errorf("expected 3 users for management databases, got %d", len(users))
	}
}

func TestGetPostgreSQLConnectionSecret(t *testing.T) {
	tests := []struct {
		username string
		expected string
	}{
		{"carbide", "carbide-postgres-pguser-carbide"},
		{"forge", "carbide-postgres-pguser-forge"},
		{"temporal", "carbide-postgres-pguser-temporal"},
		{"keycloak", "carbide-postgres-pguser-keycloak"},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			result := GetPostgreSQLConnectionSecret(tt.username)
			if result != tt.expected {
				t.Errorf("GetPostgreSQLConnectionSecret(%q) = %q, want %q", tt.username, result, tt.expected)
			}
		})
	}
}

func TestBuildPostgreSQLInitConfigMap(t *testing.T) {
	deployment := newTestDeployment(nil)

	cm := BuildPostgreSQLInitConfigMap(deployment, "test-ns")

	// Verify name
	if cm.Name != PostgreSQLInitConfigMapName {
		t.Errorf("expected name %q, got %q", PostgreSQLInitConfigMapName, cm.Name)
	}

	// Verify namespace
	if cm.Namespace != "test-ns" {
		t.Errorf("expected namespace %q, got %q", "test-ns", cm.Namespace)
	}

	// Verify init.sql key exists and contains pg_trgm extension for forge DB
	initSQL, ok := cm.Data["init.sql"]
	if !ok {
		t.Fatal("expected init.sql key in ConfigMap data")
	}

	if !strings.Contains(initSQL, `\c forge`) {
		t.Error("expected init.sql to contain '\\c forge' to connect to forge database")
	}

	if !strings.Contains(initSQL, "pg_trgm") {
		t.Error("expected init.sql to contain pg_trgm extension")
	}

	if !strings.Contains(initSQL, "CREATE EXTENSION IF NOT EXISTS pg_trgm") {
		t.Error("expected init.sql to contain 'CREATE EXTENSION IF NOT EXISTS pg_trgm'")
	}
}

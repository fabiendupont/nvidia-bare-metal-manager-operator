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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
	"github.com/NVIDIA/bare-metal-manager-operator/internal/resources"
)

const (
	PostgreSQLClusterName       = "carbide-postgres"
	PostgreSQLInitConfigMapName = "carbide-postgres-init"
)

// userDatabaseMapping maps user names to the databases they should own.
// In the per-user model, each service gets its own PostgreSQL user and database.
var userDatabaseMapping = map[string][]string{
	"carbide":  {"carbide"},
	"forge":    {"forge"},
	"rla":      {"rla"},
	"psm":      {"psm"},
	"temporal": {"temporal", "temporal_visibility"},
	"keycloak": {"keycloak"},
}

// BuildPostgreSQLCluster creates a PostgresCluster CR for postgres-operator
func BuildPostgreSQLCluster(deployment *carbitev1alpha1.CarbideDeployment, namespace string) (*unstructured.Unstructured, error) {
	pgConfig := deployment.Spec.Infrastructure.PostgreSQL

	version := pgConfig.Version
	if version == "" {
		version = "16"
	}

	replicas := pgConfig.Replicas
	if replicas == 0 {
		replicas = 1
	}

	storageSize := "10Gi"
	if pgConfig.Storage != nil {
		storageSize = pgConfig.Storage.Size.String()
	}

	storageClass := resources.GetStorageClass(deployment, pgConfig.Storage)

	// Build PostgresCluster CR
	// Using unstructured since postgres-operator CRDs may not be imported
	cluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "postgres-operator.crunchydata.com/v1beta1",
			"kind":       "PostgresCluster",
			"metadata": map[string]interface{}{
				"name":      PostgreSQLClusterName,
				"namespace": namespace,
				"labels":    resources.DefaultLabelsUnstructured("postgresql", deployment),
			},
			"spec": map[string]interface{}{
				"postgresVersion": version,
				"instances": []interface{}{
					map[string]interface{}{
						"name":     "instance1",
						"replicas": replicas,
						"dataVolumeClaimSpec": map[string]interface{}{
							"accessModes": []interface{}{"ReadWriteOnce"},
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"storage": storageSize,
								},
							},
						},
					},
				},
				"backups": map[string]interface{}{
					"pgbackrest": map[string]interface{}{
						"repos": []interface{}{
							map[string]interface{}{
								"name": "repo1",
								"volume": map[string]interface{}{
									"volumeClaimSpec": map[string]interface{}{
										"accessModes": []interface{}{"ReadWriteOnce"},
										"resources": map[string]interface{}{
											"requests": map[string]interface{}{
												"storage": "10Gi",
											},
										},
									},
								},
							},
						},
					},
				},
				"databaseInitSQL": map[string]interface{}{
					"name": PostgreSQLInitConfigMapName,
					"key":  "init.sql",
				},
				"users": buildPostgreSQLUsers(pgConfig),
			},
		},
	}

	// Add storage class if specified
	if storageClass != "" {
		spec := cluster.Object["spec"].(map[string]interface{})
		instances := spec["instances"].([]interface{})
		instance := instances[0].(map[string]interface{})
		dataVolume := instance["dataVolumeClaimSpec"].(map[string]interface{})
		dataVolume["storageClassName"] = storageClass

		backups := spec["backups"].(map[string]interface{})
		pgbackrest := backups["pgbackrest"].(map[string]interface{})
		repos := pgbackrest["repos"].([]interface{})
		repo := repos[0].(map[string]interface{})
		volume := repo["volume"].(map[string]interface{})
		volumeClaim := volume["volumeClaimSpec"].(map[string]interface{})
		volumeClaim["storageClassName"] = storageClass
	}

	// Add resources if specified
	if pgConfig.Resources != nil {
		spec := cluster.Object["spec"].(map[string]interface{})
		instances := spec["instances"].([]interface{})
		instance := instances[0].(map[string]interface{})
		instance["resources"] = convertResources(pgConfig.Resources)
	}

	// Set owner reference
	cluster.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
			Group:   carbitev1alpha1.GroupVersion.Group,
			Version: carbitev1alpha1.GroupVersion.Version,
			Kind:    "CarbideDeployment",
		}),
	})

	return cluster, nil
}

// GetPostgreSQLConnectionSecret returns the name of the PostgreSQL connection secret
// for a specific user, created by postgres-operator.
func GetPostgreSQLConnectionSecret(username string) string {
	return fmt.Sprintf("%s-pguser-%s", PostgreSQLClusterName, username)
}

// ResolveUserSecret returns the secret name for a given database user.
// In external mode, it looks up the secret from the userSecrets map.
// In managed mode, it uses the PGO convention: carbide-postgres-pguser-{user}.
func ResolveUserSecret(deployment *carbitev1alpha1.CarbideDeployment, username string) string {
	if deployment.Spec.Infrastructure != nil &&
		deployment.Spec.Infrastructure.PostgreSQL.Mode == carbitev1alpha1.ExternalMode &&
		deployment.Spec.Infrastructure.PostgreSQL.Connection != nil &&
		deployment.Spec.Infrastructure.PostgreSQL.Connection.UserSecrets != nil {
		if ref, ok := deployment.Spec.Infrastructure.PostgreSQL.Connection.UserSecrets[username]; ok {
			return ref.Name
		}
	}
	return GetPostgreSQLConnectionSecret(username)
}

// BuildPostgreSQLConnectionInfo creates a ConfigMap with PostgreSQL connection info
func BuildPostgreSQLConnectionInfo(deployment *carbitev1alpha1.CarbideDeployment, namespace, host string, port int32) *corev1.ConfigMap {
	if port == 0 {
		port = 5432
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "carbide-postgres-connection",
			Namespace: namespace,
			Labels:    resources.DefaultLabels("postgresql-config", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: map[string]string{
			"host": host,
			"port": fmt.Sprintf("%d", port),
		},
	}
}

// BuildPostgreSQLInitConfigMap creates a ConfigMap with the database init SQL.
func BuildPostgreSQLInitConfigMap(deployment *carbitev1alpha1.CarbideDeployment, namespace string) *corev1.ConfigMap {
	initSQL := `\c forge
CREATE EXTENSION IF NOT EXISTS pg_trgm;
`

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PostgreSQLInitConfigMapName,
			Namespace: namespace,
			Labels:    resources.DefaultLabels("postgresql-init", deployment),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(deployment, schema.GroupVersionKind{
					Group:   carbitev1alpha1.GroupVersion.Group,
					Version: carbitev1alpha1.GroupVersion.Version,
					Kind:    "CarbideDeployment",
				}),
			},
		},
		Data: map[string]string{
			"init.sql": initSQL,
		},
	}
}

// buildPostgreSQLUsers creates per-user configurations based on the database list.
// Each database gets its own user in the per-user model.
func buildPostgreSQLUsers(pgConfig carbitev1alpha1.PostgreSQLConfig) []interface{} {
	databases := pgConfig.Databases
	if len(databases) == 0 {
		databases = []string{"carbide", "forge"}
	}

	// Build a set of unique users needed
	usersNeeded := make(map[string][]string)
	for _, db := range databases {
		// Find which user owns this database
		found := false
		for user, dbs := range userDatabaseMapping {
			for _, mappedDB := range dbs {
				if mappedDB == db {
					usersNeeded[user] = append(usersNeeded[user], db)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			// Database not in mapping, create a user with the same name
			usersNeeded[db] = append(usersNeeded[db], db)
		}
	}

	// Deduplicate database lists per user
	users := make([]interface{}, 0, len(usersNeeded))
	for user, dbs := range usersNeeded {
		seen := make(map[string]bool)
		uniqueDBs := make([]string, 0, len(dbs))
		for _, db := range dbs {
			if !seen[db] {
				seen[db] = true
				uniqueDBs = append(uniqueDBs, db)
			}
		}
		users = append(users, map[string]interface{}{
			"name":      user,
			"databases": interfaceSliceFromStrings(uniqueDBs),
		})
	}

	return users
}

// interfaceSliceFromStrings converts a string slice to an interface slice
func interfaceSliceFromStrings(strings []string) []interface{} {
	result := make([]interface{}, len(strings))
	for i, s := range strings {
		result[i] = s
	}
	return result
}

// convertResources converts K8s ResourceRequirements to unstructured format
func convertResources(res *corev1.ResourceRequirements) map[string]interface{} {
	result := make(map[string]interface{})

	if len(res.Requests) > 0 {
		requests := make(map[string]interface{})
		for k, v := range res.Requests {
			requests[string(k)] = v.String()
		}
		result["requests"] = requests
	}

	if len(res.Limits) > 0 {
		limits := make(map[string]interface{})
		for k, v := range res.Limits {
			limits[string(k)] = v.String()
		}
		result["limits"] = limits
	}

	return result
}

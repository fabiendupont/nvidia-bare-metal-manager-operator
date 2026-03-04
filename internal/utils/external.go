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
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	carbitev1alpha1 "github.com/NVIDIA/bare-metal-manager-operator/api/v1alpha1"
)

// ValidateExternalPostgreSQL validates connectivity to an external PostgreSQL instance
func ValidateExternalPostgreSQL(ctx context.Context, c client.Client, namespace string, config *carbitev1alpha1.ExternalPGConnection) error {
	if config == nil {
		return fmt.Errorf("external PostgreSQL connection config is nil")
	}

	port := config.Port
	if port == 0 {
		port = 5432
	}

	sslMode := config.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}

	// Validate per-user secrets exist
	if len(config.UserSecrets) > 0 {
		for userName, secretRef := range config.UserSecrets {
			var secret corev1.Secret
			if err := c.Get(ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      secretRef.Name,
			}, &secret); err != nil {
				return fmt.Errorf("failed to get PostgreSQL user secret for %s: %w", userName, err)
			}
		}

		// Test connectivity using the first available user secret
		for _, secretRef := range config.UserSecrets {
			var secret corev1.Secret
			if err := c.Get(ctx, types.NamespacedName{
				Namespace: namespace,
				Name:      secretRef.Name,
			}, &secret); err != nil {
				continue
			}

			usernameKey := secretRef.UsernameKey
			if usernameKey == "" {
				usernameKey = "username"
			}
			passwordKey := secretRef.PasswordKey
			if passwordKey == "" {
				passwordKey = "password"
			}

			username := string(secret.Data[usernameKey])
			password := string(secret.Data[passwordKey])
			dbname := string(secret.Data["dbname"])
			if dbname == "" {
				dbname = "postgres"
			}

			connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=10",
				config.Host, port, username, password, dbname, sslMode)

			db, err := sql.Open("postgres", connStr)
			if err != nil {
				return fmt.Errorf("failed to open PostgreSQL connection: %w", err)
			}
			defer db.Close()

			testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			if err := db.PingContext(testCtx); err != nil {
				return fmt.Errorf("failed to ping PostgreSQL at %s:%d: %w", config.Host, port, err)
			}

			return nil
		}
	}

	// If no user secrets, just validate TCP connectivity
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(testCtx, "tcp", fmt.Sprintf("%s:%d", config.Host, port))
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL at %s:%d: %w", config.Host, port, err)
	}
	conn.Close()

	return nil
}

// ValidateExternalTemporal validates connectivity to an external Temporal instance
func ValidateExternalTemporal(ctx context.Context, endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("Temporal endpoint is empty")
	}

	// Parse endpoint to get host and port
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("invalid Temporal endpoint format %q: %w", endpoint, err)
	}

	// Test TCP connectivity
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(testCtx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("failed to connect to Temporal at %s: %w", endpoint, err)
	}
	conn.Close()

	return nil
}

// ValidateExternalKeycloak validates connectivity to an external Keycloak instance
func ValidateExternalKeycloak(ctx context.Context, c client.Client, namespace, endpoint, realm string, clientSecretRef *carbitev1alpha1.SecretRef) error {
	if endpoint == "" {
		return fmt.Errorf("Keycloak endpoint is empty")
	}
	if realm == "" {
		return fmt.Errorf("Keycloak realm is empty")
	}

	// If client secret is provided, validate it exists
	if clientSecretRef != nil {
		var secret corev1.Secret
		if err := c.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      clientSecretRef.Name,
		}, &secret); err != nil {
			return fmt.Errorf("failed to get Keycloak client secret: %w", err)
		}

		secretKey := clientSecretRef.Key
		if secretKey == "" {
			secretKey = "client-secret"
		}

		if _, ok := secret.Data[secretKey]; !ok {
			return fmt.Errorf("client secret key %q not found in secret", secretKey)
		}
	}

	// Test connectivity to Keycloak
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	// Check realm endpoint
	realmURL := fmt.Sprintf("%s/realms/%s", endpoint, realm)
	req, err := http.NewRequestWithContext(ctx, "GET", realmURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create Keycloak realm check request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Keycloak at %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Keycloak realm %q not accessible at %s (status: %d)", realm, endpoint, resp.StatusCode)
	}

	return nil
}

// GetSecretValue retrieves a value from a secret
func GetSecretValue(ctx context.Context, c client.Client, namespace string, secretRef carbitev1alpha1.SecretRef, defaultKey string) (string, error) {
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      secretRef.Name,
	}, &secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", secretRef.Name, err)
	}

	key := secretRef.Key
	if key == "" {
		key = defaultKey
	}

	value, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %s", key, secretRef.Name)
	}

	return string(value), nil
}

// Package main provides a lightweight stub server that mimics the behavior
// of Carbide services (carbide-api, carbide-rla, carbide-psm, etc.) for
// E2E testing. It responds to health/readiness probes and optionally
// runs a database migration check against PostgreSQL.
//
// Usage:
//
//	stub-server [flags]
//	  --grpc-port    gRPC listen port (default: 1079)
//	  --http-port    HTTP listen port for health/metrics (default: 1080)
//	  --check-db     If set, verify DB connectivity on startup
//	  --migrate      If set, run a fake migration (create a marker table)
//	  --name         Service name for logging (default: "stub")
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/lib/pq"
)

func main() {
	grpcPort := flag.Int("grpc-port", 1079, "gRPC listen port")
	httpPort := flag.Int("http-port", 1080, "HTTP listen port (health/metrics)")
	checkDB := flag.Bool("check-db", false, "Check database connectivity on startup")
	migrate := flag.Bool("migrate", false, "Run fake database migration")
	name := flag.String("name", "stub", "Service name")
	flag.Parse()

	log.Printf("[%s] starting stub server (grpc=%d, http=%d, checkDB=%v, migrate=%v)",
		*name, *grpcPort, *httpPort, *checkDB, *migrate)

	// Database connectivity check
	if *checkDB || *migrate {
		dbURL := os.Getenv("CARBIDE_API_DATABASE_URL")
		if dbURL == "" {
			// Build from individual vars
			host := envOrDefault("DB_ADDR", envOrDefault("DB_HOST", "localhost"))
			port := envOrDefault("DB_PORT", "5432")
			user := envOrDefault("DB_USER", "carbide")
			pass := envOrDefault("DB_PASSWORD", "")
			dbname := envOrDefault("DB_DATABASE", envOrDefault("DB_NAME", "carbide"))
			sslmode := envOrDefault("DB_SSLMODE", "disable")
			dbURL = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
				host, port, user, pass, dbname, sslmode)
		}

		db, err := sql.Open("postgres", dbURL)
		if err != nil {
			log.Fatalf("[%s] failed to open database: %v", *name, err)
		}
		defer db.Close() //nolint:errcheck

		if err := db.Ping(); err != nil {
			log.Fatalf("[%s] failed to connect to database: %v", *name, err)
		}
		log.Printf("[%s] database connectivity OK", *name)

		if *migrate {
			_, err := db.Exec(fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s_migration_marker (id serial PRIMARY KEY, created_at timestamp DEFAULT now())",
				*name))
			if err != nil {
				log.Fatalf("[%s] migration failed: %v", *name, err)
			}
			log.Printf("[%s] migration complete", *name)

			// If only migrating (like init container), exit after migration
			if !*checkDB && *grpcPort == 0 {
				os.Exit(0)
			}
		}
	}

	// Start a dummy gRPC listener (just accepts TCP connections)
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", *grpcPort))
	if err != nil {
		log.Fatalf("[%s] failed to listen on gRPC port %d: %v", *name, *grpcPort, err)
	}
	defer grpcListener.Close() //nolint:errcheck
	log.Printf("[%s] gRPC listener on :%d", *name, *grpcPort)

	// Accept connections in background (keeps the port open for probes)
	go func() {
		for {
			conn, err := grpcListener.Accept()
			if err != nil {
				return
			}
			conn.Close() //nolint:errcheck
		}
	}()

	// HTTP health/metrics server
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ok","service":"%s"}`, *name)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ready","service":"%s"}`, *name)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "# HELP stub_up Stub service is up\n# TYPE stub_up gauge\nstub_up{service=\"%s\"} 1\n", *name)
	})

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *httpPort),
		Handler: mux,
	}

	go func() {
		log.Printf("[%s] HTTP health/metrics on :%d", *name, *httpPort)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("[%s] HTTP server error: %v", *name, err)
		}
	}()

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("[%s] received %v, shutting down", *name, sig)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

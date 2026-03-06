// Package main provides a lightweight stub server that mimics the behavior
// of Carbide services (carbide-api, carbide-rla, carbide-psm, etc.) for
// E2E testing. It responds to health/readiness probes and optionally
// runs a database migration check against PostgreSQL.
//
// The stub auto-detects which service it's mimicking based on the binary
// name (via symlinks) or the --name flag, and configures ports accordingly.
// Unknown CLI args are ignored so the same binary works as a drop-in
// replacement for any Carbide service command.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	_ "github.com/lib/pq"
)

// serviceConfig maps binary/service names to default port configurations.
var serviceConfig = map[string][2]int{
	"carbide-api":  {1079, 1080},
	"carbide":      {1079, 1080}, // PXE binary name
	"rla":          {50051, 50052},
	"psm":          {50051, 50052},
	"carbide-rest": {8080, 9360},
	"stub-server":  {1079, 1080},
	"stub":         {1079, 1080},
}

func main() {
	// Detect service name from binary name (supports symlinks)
	binName := filepath.Base(os.Args[0])
	svcName := binName

	// Auto-configure ports based on binary name
	grpcPort, httpPort := 1079, 1080
	if ports, ok := serviceConfig[binName]; ok {
		grpcPort, httpPort = ports[0], ports[1]
	}

	// Override from environment if set
	if p := os.Getenv("STUB_GRPC_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &grpcPort)
	}
	if p := os.Getenv("STUB_HTTP_PORT"); p != "" {
		_, _ = fmt.Sscanf(p, "%d", &httpPort)
	}
	if n := os.Getenv("STUB_NAME"); n != "" {
		svcName = n
	}

	// Check for "migrate" subcommand — run DB check and exit
	isMigrate := false
	for _, arg := range os.Args[1:] {
		if arg == "migrate" || arg == "db" {
			isMigrate = true
			break
		}
	}

	// Parse --port flag from args (for RLA/PSM: serve --port 50051)
	for i, arg := range os.Args[1:] {
		if arg == "--port" && i+2 < len(os.Args) {
			_, _ = fmt.Sscanf(os.Args[i+2], "%d", &grpcPort)
		}
	}

	log.Printf("[%s] starting stub server (grpc=%d, http=%d, migrate=%v)",
		svcName, grpcPort, httpPort, isMigrate)

	// Database connectivity check / migration
	dbURL := os.Getenv("CARBIDE_API_DATABASE_URL")
	if dbURL == "" {
		host := envOrDefault("DB_ADDR", envOrDefault("DB_HOST", ""))
		if host != "" {
			port := envOrDefault("DB_PORT", "5432")
			user := envOrDefault("DB_USER", "carbide")
			pass := envOrDefault("DB_PASSWORD", "")
			dbname := envOrDefault("DB_DATABASE", envOrDefault("DB_NAME", "carbide"))
			sslmode := envOrDefault("DB_SSLMODE", "disable")
			dbURL = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
				host, port, user, pass, dbname, sslmode)
		}
	}

	if dbURL != "" {
		db, err := sql.Open("postgres", dbURL)
		if err != nil {
			if isMigrate {
				log.Fatalf("[%s] failed to open database: %v", svcName, err)
			}
			log.Printf("[%s] warning: failed to open database: %v", svcName, err)
		} else {
			defer db.Close() //nolint:errcheck
			if err := db.Ping(); err != nil {
				if isMigrate {
					log.Fatalf("[%s] failed to connect to database: %v", svcName, err)
				}
				log.Printf("[%s] warning: failed to ping database: %v", svcName, err)
			} else {
				log.Printf("[%s] database connectivity OK", svcName)
				if isMigrate {
					table := strings.ReplaceAll(svcName, "-", "_") + "_migrations"
					_, _ = db.Exec(fmt.Sprintf(
						"CREATE TABLE IF NOT EXISTS %s (id serial PRIMARY KEY, applied_at timestamp DEFAULT now())", table))
					log.Printf("[%s] migration complete (table: %s)", svcName, table)
				}
			}
		}
	}

	if isMigrate {
		log.Printf("[%s] migration mode — exiting", svcName)
		os.Exit(0)
	}

	// Start a TCP listener on the gRPC port (accepts connections for probes)
	grpcListener, err := net.Listen("tcp", fmt.Sprintf(":%d", grpcPort))
	if err != nil {
		log.Fatalf("[%s] failed to listen on port %d: %v", svcName, grpcPort, err)
	}
	defer grpcListener.Close() //nolint:errcheck
	log.Printf("[%s] listening on :%d", svcName, grpcPort)

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
		_, _ = fmt.Fprintf(w, `{"status":"ok","service":"%s"}`, svcName)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ready","service":"%s"}`, svcName)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "# HELP stub_up Stub service is up\n# TYPE stub_up gauge\nstub_up{service=\"%s\"} 1\n", svcName)
	})

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", httpPort),
		Handler: mux,
	}

	go func() {
		log.Printf("[%s] HTTP health/metrics on :%d", svcName, httpPort)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("[%s] HTTP server error: %v (non-fatal)", svcName, err)
		}
	}()

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("[%s] received %v, shutting down", svcName, sig)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

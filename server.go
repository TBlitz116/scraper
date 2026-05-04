package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// openDB opens a connection to the scheduler Postgres database, applying the
// same URL normalisation used by storeFaculty (async driver prefix, docker
// hostname, exposed port, sslmode).
func openDB() (*sql.DB, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is not set")
	}
	dbURL = strings.Replace(dbURL, "postgresql+asyncpg://", "postgresql://", 1)
	dbURL = strings.Replace(dbURL, "@db:", "@localhost:", 1)
	dbURL = strings.Replace(dbURL, ":5432/", ":5433/", 1)
	if !strings.Contains(dbURL, "sslmode=") {
		if strings.Contains(dbURL, "?") {
			dbURL += "&sslmode=disable"
		} else {
			dbURL += "?sslmode=disable"
		}
	}
	return sql.Open("postgres", dbURL)
}

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Time     string `json:"time"`
	Error    string `json:"error,omitempty"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:   "ok",
		Database: "ok",
		Time:     time.Now().UTC().Format(time.RFC3339),
	}
	status := http.StatusOK

	db, err := openDB()
	if err != nil {
		resp.Status = "degraded"
		resp.Database = "unavailable"
		resp.Error = err.Error()
		status = http.StatusServiceUnavailable
	} else {
		defer db.Close()
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			resp.Status = "degraded"
			resp.Database = "down"
			resp.Error = err.Error()
			status = http.StatusServiceUnavailable
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func runServer() error {
	addr := os.Getenv("SCRAPER_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("scraper-go listening on %s", addr)
	return srv.ListenAndServe()
}

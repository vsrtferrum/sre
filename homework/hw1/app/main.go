package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	version   = "dev"
	readyFlag atomic.Bool
	inflight  atomic.Int64
)

func init() {
	readyFlag.Store(true)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		hostname, _ := os.Hostname()
		inflight.Add(1)
		defer inflight.Add(-1)
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message":  "hello",
			"version":  version,
			"pod":      hostname,
			"instance": fmt.Sprintf("%s@%s", version, hostname),
		})
	})

	mux.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if readyFlag.Load() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ready")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, "not ready")
	})

	mux.HandleFunc("/unready", func(w http.ResponseWriter, r *http.Request) {
		readyFlag.Store(false)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "flag set to not-ready")
	})

	mux.HandleFunc("/ready-on", func(w http.ResponseWriter, r *http.Request) {
		readyFlag.Store(true)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "flag set to ready")
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
		sig := <-quit
		log.Printf("received %s, draining (inflight=%d)...", sig, inflight.Load())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		log.Printf("shutdown complete")
	}()

	log.Printf("app version=%s listening on :%s", version, port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

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

const (
	oldGreeting = "hello"
	newGreeting = "hello from the new greeting"
)

var (
	version   = "dev"
	readyFlag atomic.Bool
	inflight  atomic.Int64
)

func init() { readyFlag.Store(true) }

type featureFlag struct{ newGreeting atomic.Bool }

func (f *featureFlag) enabled() bool    { return f.newGreeting.Load() }
func (f *featureFlag) set(enabled bool) { f.newGreeting.Store(enabled) }

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	hostname, _ := os.Hostname()
	inflight.Add(1)
	defer inflight.Add(-1)
	time.Sleep(20 * time.Millisecond)
	writeJSON(w, map[string]string{
		"message": oldGreeting, "version": version, "pod": hostname,
		"instance": fmt.Sprintf("%s@%s", version, hostname),
	})
}

func messageHandler(flag *featureFlag) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		greeting := oldGreeting
		if flag.enabled() {
			greeting = newGreeting
		}
		writeJSON(w, map[string]any{"message": greeting, "new_greeting": flag.enabled()})
	}
}

func featureHandler(flag *featureFlag) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var enabled bool
		switch r.URL.Query().Get("enabled") {
		case "true":
			enabled = true
		case "false":
			enabled = false
		default:
			http.Error(w, "enabled must be true or false", http.StatusBadRequest)
			return
		}
		flag.set(enabled)
		writeJSON(w, map[string]any{"new_greeting": enabled, "updated": true})
	}
}

func statusHandler(flag *featureFlag) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{"new_greeting": flag.enabled()})
	}
}

func publicMux(flag *featureFlag) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.Handle("/message", messageHandler(flag))
	mux.Handle("/feature-status", statusHandler(flag))
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
		fmt.Fprintln(w, "flag set to not-ready")
	})
	mux.HandleFunc("/ready-on", func(w http.ResponseWriter, r *http.Request) {
		readyFlag.Store(true)
		fmt.Fprintln(w, "flag set to ready")
	})
	return mux
}

func adminMux(flag *featureFlag) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/admin/feature", featureHandler(flag))
	mux.Handle("/admin/feature/status", statusHandler(flag))
	return mux
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	adminPort := os.Getenv("ADMIN_PORT")
	if adminPort == "" {
		adminPort = "8081"
	}

	flag := &featureFlag{}
	publicServer := &http.Server{Addr: ":" + port, Handler: publicMux(flag), ReadHeaderTimeout: 5 * time.Second}
	adminServer := &http.Server{Addr: ":" + adminPort, Handler: adminMux(flag), ReadHeaderTimeout: 5 * time.Second}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-stop
		log.Printf("received %s, draining (inflight=%d)...", sig, inflight.Load())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := publicServer.Shutdown(ctx); err != nil {
			log.Printf("public shutdown error: %v", err)
		}
		if err := adminServer.Shutdown(ctx); err != nil {
			log.Printf("admin shutdown error: %v", err)
		}
		log.Printf("shutdown complete")
	}()

	go func() {
		if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("admin server error: %v", err)
		}
	}()
	log.Printf("app version=%s listening on :%s (public), :%s (admin-only)", version, port, adminPort)
	if err := publicServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

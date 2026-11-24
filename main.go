// Quartiers: a multiplayer property-trading board game for the browser. This is its server: the
// rooms (internal/server), the rules (internal/engine) and a bare test client (web/).
package main

import (
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"quartiers/internal/engine"
	"quartiers/internal/server"
)

//go:embed web
var web embed.FS

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n > 0 {
		return n
	}
	return def
}

func main() {
	// `quartiers healthcheck`, for Docker (an image without a shell): is the server accepting connections?
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+env("PORT", "3000"), 2*time.Second)
		if err != nil {
			os.Exit(1)
		}
		conn.Close()
		return
	}

	hub := server.NewHub(server.Config{
		MaxRooms:   envInt("MAX_ROOMS", 200),
		TrustProxy: os.Getenv("TRUST_PROXY") == "1",
	}, engine.Classic)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /ws", hub.ServeWS)
	client, _ := fs.Sub(web, "web")
	files := http.FileServerFS(client)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
	srv := &http.Server{
		Addr:              net.JoinHostPort(env("HOST", "0.0.0.0"), env("PORT", "3000")),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}

	// On a stop the players are told the server restarts (net/http knows nothing of websockets).
	stop, done := make(chan os.Signal, 1), make(chan struct{})
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		srv.Close()
		hub.Close()
		close(done)
	}()
	log.Printf("quartiers: http://%s", srv.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-done
}

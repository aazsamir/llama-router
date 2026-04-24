package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	// CLI flags
	wrapperPort := flag.Int("port", 11434, "Port for the wrapper to listen on")
	ttl := flag.Duration("ttl", 180*time.Second, "Time to live - stop llama-server after this idle duration")
	llamaServer := flag.String("llama-server", "llama-server", "Path to llama-server binary")
	preset := flag.String("preset", "preset.ini", "Path to preset.ini file")
	flag.Parse()

	// Resolve paths
	absPreset, err := filepath.Abs(*preset)
	if err != nil {
		log.Fatalf("resolve preset path: %v", err)
	}

	// Check if preset exists
	if _, err := os.Stat(absPreset); os.IsNotExist(err) {
		log.Fatalf("preset file not found: %s", absPreset)
	}

	// Resolve llama-server path (searches PATH)
	llamaServerPath, err := exec.LookPath(*llamaServer)
	if err != nil {
		log.Fatalf("llama-server not found in PATH: %v", err)
	}

	log.Printf("=== llama-router ===")
	log.Printf("Wrapper port:  %d", *wrapperPort)
	log.Printf("Backend port:  %d", *wrapperPort+1)
	log.Printf("TTL:           %v", *ttl)
	log.Printf("llama-server:  %s", llamaServerPath)
	log.Printf("Preset:        %s", absPreset)
	log.Printf("===================")

	// Create process manager
	pm := NewProcessManager(llamaServerPath, absPreset, *wrapperPort)

	// Create proxy
	proxy, err := NewProxy(pm.WrapperAddr(), pm.BackendAddr(), pm, *ttl)
	if err != nil {
		log.Fatalf("create proxy: %v", err)
	}

	// Start the proxy
	if err := proxy.Start(); err != nil {
		log.Fatalf("start proxy: %v", err)
	}

	log.Printf("llama-router is ready. Access llama-server at http://0.0.0.0:%d", *wrapperPort)

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Printf("received %v, shutting down...", sig)

	if err := proxy.Stop(); err != nil {
		log.Printf("error during shutdown: %v", err)
	}

	fmt.Println("llama-router stopped")
}

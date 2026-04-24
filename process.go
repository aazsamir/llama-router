package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type ProcessManager struct {
	cmd        *exec.Cmd
	mu         sync.Mutex
	pid        int32
	backendURL string
	// Path to llama-server binary
	llamaServerPath string
	// Base port for the backend (wrapper listens on wrapperPort, backend runs on backendPort)
	basePort int
	// Preset file path
	presetPath string
	// readyCh is closed when llama-server becomes ready
	readyCh chan struct{}
	// startOnce ensures only one Start call is effective
	startOnce sync.Once
	// closingCh is closed when the process is shutting down
	closingCh chan struct{}
}

func NewProcessManager(llamaServerPath string, presetPath string, basePort int) *ProcessManager {
	return &ProcessManager{
		llamaServerPath: llamaServerPath,
		presetPath:      presetPath,
		basePort:        basePort,
		readyCh:         make(chan struct{}),
		closingCh:       make(chan struct{}),
	}
}

func (pm *ProcessManager) BackendAddr() string {
	return fmt.Sprintf("http://127.0.0.1:%d", pm.backendPort())
}

func (pm *ProcessManager) backendPort() int {
	return pm.basePort + 1
}

func (pm *ProcessManager) WrapperAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", pm.basePort)
}

func (pm *ProcessManager) Start() error {
	var startErr error
	pm.startOnce.Do(func() {
		pm.mu.Lock()
		defer pm.mu.Unlock()

		// Check if already running
		if atomic.LoadInt32(&pm.pid) != 0 {
			log.Println("llama-server already running")
			return
		}

		port := pm.backendPort()
		args := []string{
			"--models-max", "1",
			"--models-preset", pm.presetPath,
			"--host", "0.0.0.0",
			"--port", fmt.Sprintf("%d", port),
			"-np", "1",
		}

		log.Printf("starting llama-server: %s %v", pm.llamaServerPath, args)

		pm.cmd = exec.Command(pm.llamaServerPath, args...)
		pm.cmd.Stdout = os.Stdout
		pm.cmd.Stderr = os.Stderr

		if err := pm.cmd.Start(); err != nil {
			startErr = fmt.Errorf("start llama-server: %w", err)
			return
		}

		atomic.StoreInt32(&pm.pid, int32(pm.cmd.Process.Pid))
		log.Printf("llama-server started with PID %d on port %d", pm.cmd.Process.Pid, port)

		// Wait for server to be ready
		go pm.waitForReady()
	})

	return startErr
}

func (pm *ProcessManager) ResetForRestart() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.readyCh = make(chan struct{})
	pm.closingCh = make(chan struct{})
	pm.startOnce = sync.Once{}
	pm.pid = 0
	pm.cmd = nil
}

func (pm *ProcessManager) waitForReady() {
	maxRetries := 60
	retryInterval := 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		select {
		case <-pm.closingCh:
			log.Println("shutdown requested, stopping readiness check")
			return
		default:
		}

		time.Sleep(retryInterval)

		pm.mu.Lock()
		pid := atomic.LoadInt32(&pm.pid)
		pm.mu.Unlock()

		if pid == 0 {
			return
		}

		// Try to connect to the backend
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", pm.backendPort()))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				log.Printf("llama-server is ready on port %d", pm.backendPort())
				close(pm.readyCh)
				return
			}
		}

		// Check if process is still alive
		pm.mu.Lock()
		cmd := pm.cmd
		pm.mu.Unlock()

		if cmd == nil || cmd.ProcessState != nil {
			log.Println("llama-server exited before becoming ready")
			return
		}
	}

	log.Println("warning: llama-server may not be ready yet")
	close(pm.readyCh)
}

func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pid := atomic.LoadInt32(&pm.pid)
	if pid == 0 {
		log.Println("llama-server not running, nothing to stop")
		return nil
	}

	log.Printf("stopping llama-server (PID %d)", pid)

	// Signal waitForReady to stop polling
	close(pm.closingCh)

	// Send SIGTERM first for graceful shutdown
	if pm.cmd != nil && pm.cmd.Process != nil {
		if err := pm.cmd.Process.Signal(os.Interrupt); err != nil {
			log.Printf("error sending SIGTERM: %v, trying SIGKILL", err)
			pm.cmd.Process.Kill()
		}
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- pm.cmd.Wait()
	}()

	select {
	case <-done:
		log.Println("llama-server stopped gracefully")
	case <-time.After(10 * time.Second):
		log.Println("llama-server didn't stop in time, killing...")
		pm.cmd.Process.Kill()
		<-done
	}

	atomic.StoreInt32(&pm.pid, 0)
	pm.cmd = nil

	return nil
}

func (pm *ProcessManager) IsRunning() bool {
	return atomic.LoadInt32(&pm.pid) != 0
}

func (pm *ProcessManager) Ready() <-chan struct{} {
	return pm.readyCh
}

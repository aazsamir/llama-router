package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

type Proxy struct {
	proxy        *httputil.ReverseProxy
	server       *http.Server
	proc         *ProcessManager
	ttl          time.Duration
	mu           sync.Mutex
	lastReq      time.Time
	running      bool
	ctx          context.Context
	cancelFunc   context.CancelFunc
	ticker       *time.Ticker
	tickerCtx    context.Context
	tickerCancel context.CancelFunc
}

func NewProxy(addr string, backendURL string, proc *ProcessManager, ttl time.Duration) (*Proxy, error) {
	backend, err := url.Parse(backendURL)
	if err != nil {
		return nil, fmt.Errorf("parse backend URL: %w", err)
	}

	p := &Proxy{
		proc:    proc,
		ttl:     ttl,
		running: false,
	}

	transport := &http.Transport{
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     90 * time.Second,
	}

	p.proxy = &httputil.ReverseProxy{
		Transport: transport,
		ModifyResponse: func(resp *http.Response) error {
			p.mu.Lock()
			p.lastReq = time.Now()
			p.mu.Unlock()
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy error: %v", err)
			p.mu.Lock()
			p.running = false
			p.mu.Unlock()
			http.Error(w, "llama-server unavailable", http.StatusBadGateway)
		},
	}

	// Custom director that tracks request time and sets host
	director := func(req *http.Request) {
		p.mu.Lock()
		p.lastReq = time.Now()
		p.mu.Unlock()
		req.URL.Scheme = backend.Scheme
		req.URL.Host = backend.Host
		req.Host = backend.Host
	}
	p.proxy.Director = director

	p.server = &http.Server{
		Addr:    addr,
		Handler: p,
	}

	p.ctx, p.cancelFunc = context.WithCancel(context.Background())

	return p, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	isRunning := p.running
	p.mu.Unlock()

	if !isRunning {
		log.Println("llama-server not running, starting...")
		if err := p.proc.Start(); err != nil {
			http.Error(w, fmt.Sprintf("failed to start llama-server: %v", err), http.StatusBadGateway)
			return
		}
		p.mu.Lock()
		p.running = true
		p.lastReq = time.Now()
		p.mu.Unlock()
	}

	p.mu.Lock()
	p.lastReq = time.Now()
	p.mu.Unlock()
	p.proxy.ServeHTTP(w, r)
}

func (p *Proxy) Start() error {
	log.Printf("llama-router listening on %s, forwarding to %s", p.server.Addr, p.proc.BackendAddr())
	log.Println("waiting for first request to launch llama-server...")

	p.startTTLChecker()

	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("proxy server error: %v", err)
		}
	}()

	return nil
}

func (p *Proxy) startTTLChecker() {
	p.ticker = time.NewTicker(5 * time.Second)
	p.tickerCtx, p.tickerCancel = context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case <-p.ticker.C:
				p.checkTTL()
			case <-p.tickerCtx.Done():
				return
			}
		}
	}()
}

func (p *Proxy) checkTTL() {
	p.mu.Lock()
	lastReq := p.lastReq
	running := p.running
	p.mu.Unlock()

	if !running {
		return
	}

	if time.Since(lastReq) > p.ttl {
		log.Printf("TTL expired (%v idle), stopping llama-server to free memory", p.ttl)
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()

		if err := p.proc.Stop(); err != nil {
			log.Printf("error stopping llama-server: %v", err)
		}
	}
}

func (p *Proxy) Stop() error {
	p.tickerCancel()
	p.ticker.Stop()
	p.cancelFunc()

	p.mu.Lock()
	p.running = false
	p.mu.Unlock()

	if err := p.proc.Stop(); err != nil {
		log.Printf("error stopping process: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.server.Shutdown(ctx)
}

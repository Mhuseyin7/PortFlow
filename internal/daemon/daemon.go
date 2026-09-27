// Package daemon composes the store, proxy, hosts adapter and API into
// a single long-running process.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/portflow/portflow/internal/api"
	"github.com/portflow/portflow/internal/hosts"
	"github.com/portflow/portflow/internal/proxy"
	"github.com/portflow/portflow/internal/registry"
	pfca "github.com/portflow/portflow/internal/tls"
)

// Options is passed by `portflow daemon start`.
type Options struct {
	DataDir              string
	HTTPAddr             string
	HTTPSAddr            string
	APIAddr              string
	Version              string
	Commit               string
	AllowExternalTargets bool
	// SkipHosts turns off hosts-file management. Default (false) means
	// the daemon writes hostname entries into the OS hosts file.
	SkipHosts bool
}

// Daemon is a single runnable instance.
type Daemon struct {
	opts   Options
	store  *registry.Store
	ca     *pfca.CA
	certs  *pfca.CertStore
	hosts  *hosts.Manager
	proxy  *proxy.Server
	logger *log.Logger
}

// New wires all subsystems together but does not start any listeners.
func New(opts Options) (*Daemon, error) {
	if opts.APIAddr == "" {
		opts.APIAddr = "127.0.0.1:9280"
	}
	logger := log.New(os.Stderr, "portflow ", log.LstdFlags|log.Lmsgprefix)
	store, err := registry.Open(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	ca, err := pfca.LoadOrCreateCA(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("ca: %w", err)
	}
	certs, err := pfca.NewCertStore(ca, opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("cert store: %w", err)
	}
	var hm *hosts.Manager
	if !opts.SkipHosts {
		hm = hosts.NewManager()
	}
	prx := proxy.New(proxy.Options{
		HTTPAddr:             opts.HTTPAddr,
		HTTPSAddr:            opts.HTTPSAddr,
		Resolver:             store,
		CertStore:            certs,
		AllowExternalTargets: opts.AllowExternalTargets,
		Logger:               logger,
	})
	return &Daemon{
		opts:   opts,
		store:  store,
		ca:     ca,
		certs:  certs,
		hosts:  hm,
		proxy:  prx,
		logger: logger,
	}, nil
}

// Run blocks until ctx is cancelled or a listener fails.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	apiSrv := api.New(api.Deps{
		Store:        d.store,
		Hosts:        d.hosts,
		CA:           d.ca,
		CertStore:    d.certs,
		Version:      d.opts.Version,
		Commit:       d.opts.Commit,
		StartedAt:    time.Now(),
		ProxyHTTP:    d.opts.HTTPAddr,
		ProxyHTTPS:   d.opts.HTTPSAddr,
		APIAddr:      d.opts.APIAddr,
		ShutdownFunc: cancel,
		ServiceCount: func() int {
			list, _ := d.store.List()
			return len(list)
		},
	})

	// If hosts management is on, seed the file with current services.
	if d.hosts != nil {
		svcs, _ := d.store.List()
		names := make([]string, 0, len(svcs))
		for _, s := range svcs {
			names = append(names, s.Hostname)
		}
		if err := d.hosts.Set(names); err != nil {
			d.logger.Printf("hosts: %v (continuing; run daemon with elevated privileges to manage hosts)", err)
		}
	}

	// Health-check loop.
	go d.healthLoop(ctx)

	// API server.
	apiHTTP := &http.Server{
		Addr:              d.opts.APIAddr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", d.opts.APIAddr)
	if err != nil {
		return fmt.Errorf("api listen %s: %w", d.opts.APIAddr, err)
	}
	go func() {
		if err := apiHTTP.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.logger.Printf("api server: %v", err)
			cancel()
		}
	}()

	// Proxy servers.
	errCh := make(chan error, 1)
	go func() { errCh <- d.proxy.Run(ctx) }()

	<-ctx.Done()
	shutdownCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
	defer sc()
	_ = apiHTTP.Shutdown(shutdownCtx)
	_ = d.store.Close()
	return <-errCh
}

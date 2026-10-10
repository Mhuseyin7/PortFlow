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
	"strings"
	"time"

	"github.com/portflow/portflow/internal/api"
	"github.com/portflow/portflow/internal/hostname"
	"github.com/portflow/portflow/internal/hosts"
	"github.com/portflow/portflow/internal/proxy"
	"github.com/portflow/portflow/internal/registry"
	"github.com/portflow/portflow/internal/reqlog"
	pfca "github.com/portflow/portflow/internal/tls"
	"github.com/portflow/portflow/internal/trust"
)

// requestLogCapacity bounds the in-memory request log.
const requestLogCapacity = 500

// Options is passed by `portflow daemon start`.
type Options struct {
	DataDir string

	// HTTPAddr and HTTPSAddr are comma-separated listen addresses for the
	// proxy; APIAddr is a single address for the management API.
	HTTPAddr  string
	HTTPSAddr string
	APIAddr   string

	Version string
	Commit  string

	// AllowLAN permits the proxy to listen on non-loopback addresses. The
	// management API is always loopback-only.
	AllowLAN bool
	// AllowExternalTargets permits registering non-loopback proxy targets.
	AllowExternalTargets bool
	// PreserveHost forwards the original Host header upstream.
	PreserveHost bool
	// SkipHosts turns off hosts-file management.
	SkipHosts bool
	// RequestLog starts with the development request log enabled.
	RequestLog bool
}

// Daemon is a single runnable instance.
type Daemon struct {
	opts   Options
	store  *registry.Store
	ca     *pfca.CA
	certs  *pfca.CertStore
	hosts  *hosts.Manager
	reqlog *reqlog.Log
	proxy  *proxy.Server
	logger *log.Logger
}

// checkLoopback verifies that every address in the comma-separated spec binds
// only to a loopback interface. Wildcard and empty hosts are rejected.
func checkLoopback(spec string) error {
	for _, addr := range strings.Split(spec, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("invalid listen address %q: %w", addr, err)
		}
		if !hostname.IsLoopbackHost(host) {
			return fmt.Errorf("%q is not a loopback address; use 127.0.0.1 or [::1] (the proxy only listens on other interfaces with --lan)", addr)
		}
	}
	return nil
}

func validateBind(opts Options) error {
	if opts.APIAddr == "" || strings.Contains(opts.APIAddr, ",") {
		return errors.New("the management API needs exactly one listen address")
	}
	if err := checkLoopback(opts.APIAddr); err != nil {
		return fmt.Errorf("management API: %w", err) // never exposed, even with --lan
	}
	if opts.AllowLAN {
		return nil
	}
	if err := checkLoopback(opts.HTTPAddr); err != nil {
		return fmt.Errorf("http proxy: %w", err)
	}
	if err := checkLoopback(opts.HTTPSAddr); err != nil {
		return fmt.Errorf("https proxy: %w", err)
	}
	return nil
}

// New wires all subsystems together but does not start any listeners.
func New(opts Options) (*Daemon, error) {
	if opts.APIAddr == "" {
		opts.APIAddr = "127.0.0.1:9280"
	}
	if err := validateBind(opts); err != nil {
		return nil, err
	}
	logger := log.New(os.Stderr, "portflow ", log.LstdFlags|log.Lmsgprefix)
	store, err := registry.Open(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	ca, err := pfca.LoadOrCreateCA(opts.DataDir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("ca: %w", err)
	}
	certs, err := pfca.NewCertStore(ca, opts.DataDir)
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("cert store: %w", err)
	}
	var hm *hosts.Manager
	if !opts.SkipHosts {
		hm = hosts.NewManager()
	}
	rl := reqlog.New(requestLogCapacity)
	rl.SetEnabled(opts.RequestLog)
	prx := proxy.New(proxy.Options{
		HTTPAddr:             opts.HTTPAddr,
		HTTPSAddr:            opts.HTTPSAddr,
		Resolver:             store,
		CertStore:            certs,
		AllowExternalTargets: opts.AllowExternalTargets,
		PreserveHost:         opts.PreserveHost,
		Log:                  rl,
		Logger:               logger,
	})
	return &Daemon{
		opts:   opts,
		store:  store,
		ca:     ca,
		certs:  certs,
		hosts:  hm,
		reqlog: rl,
		proxy:  prx,
		logger: logger,
	}, nil
}

// syncHosts rewrites the managed hosts block from the registry. Rows that no
// longer pass validation (for example registered by an older version) are
// skipped with a warning instead of blocking every other entry.
func (d *Daemon) syncHosts() {
	if d.hosts == nil {
		return
	}
	svcs, _ := d.store.List()
	names := make([]string, 0, len(svcs))
	for _, s := range svcs {
		if hostname.Validate(s.Hostname) != nil || !hostname.AllowedSuffix(s.Hostname, nil) {
			d.logger.Printf("hosts: ignoring registered hostname %q (not a valid development name); remove it with `portflow remove`", s.Hostname)
			continue
		}
		names = append(names, s.Hostname)
	}
	if err := d.hosts.Set(names); err != nil {
		d.logger.Printf("hosts: %v (continuing; run the daemon with permission to edit %s to manage names)", err, d.hosts.Path())
	}
}

// Run blocks until ctx is cancelled or the proxy or API fails.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer d.store.Close()

	apiLn, err := net.Listen("tcp", d.opts.APIAddr)
	if err != nil {
		return fmt.Errorf("api listen %s: %w", d.opts.APIAddr, err)
	}
	d.syncHosts()

	apiSrv := api.New(api.Deps{
		Store:     d.store,
		Hosts:     d.hosts,
		CA:        d.ca,
		CertStore: d.certs,
		Log:       d.reqlog,
		Trusted: func() bool {
			ok, _ := trust.IsInstalled(d.ca.CertPath())
			return ok
		},
		AllowExternalTargets: d.opts.AllowExternalTargets,
		Version:              d.opts.Version,
		Commit:               d.opts.Commit,
		StartedAt:            time.Now(),
		ProxyHTTP:            d.opts.HTTPAddr,
		ProxyHTTPS:           d.opts.HTTPSAddr,
		APIAddr:              d.opts.APIAddr,
		ShutdownFunc:         cancel,
	})
	apiHTTP := &http.Server{
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          d.logger,
	}

	go d.healthLoop(ctx)

	apiDone := make(chan error, 1)
	go func() {
		err := apiHTTP.Serve(apiLn)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		apiDone <- err
	}()
	proxyDone := make(chan error, 1)
	go func() { proxyDone <- d.proxy.Run(ctx) }()

	var runErr error
	proxyFinished := false
	select {
	case <-ctx.Done():
	case err := <-proxyDone:
		proxyFinished, runErr = true, err
	case err := <-apiDone:
		runErr = err
	}
	cancel()

	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = apiHTTP.Shutdown(shutdownCtx)
	if !proxyFinished {
		if err := <-proxyDone; runErr == nil {
			runErr = err
		}
	}
	return runErr
}

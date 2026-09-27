package proxy

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/portflow/portflow/internal/registry"
)

// proxyWebSocket transparently tunnels a WS upgrade to the upstream target.
// It hijacks the client TCP connection, opens a matching connection to the
// backend, replays the client's Upgrade request there, and then copies
// bytes in both directions until either side closes.
func proxyWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL, _ *registry.Service, logger *log.Logger) {
	if logger == nil {
		logger = log.Default()
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket: response writer does not support hijacking", http.StatusInternalServerError)
		return
	}
	upstream, err := net.DialTimeout("tcp", target.Host, 5*time.Second)
	if err != nil {
		http.Error(w, "websocket: cannot reach upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Rewrite the request line to what the backend expects, then forward.
	outReq := r.Clone(r.Context())
	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host
	outReq.Host = target.Host
	stripHopByHop(outReq.Header)
	// Re-add the required websocket hop-by-hop headers.
	outReq.Header.Set("Connection", "Upgrade")
	outReq.Header.Set("Upgrade", "websocket")

	if err := outReq.Write(upstream); err != nil {
		upstream.Close()
		http.Error(w, "websocket: forward failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Read the backend's response headers and pipe them back verbatim.
	br := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(br, outReq)
	if err != nil {
		upstream.Close()
		http.Error(w, "websocket: bad upstream response: "+err.Error(), http.StatusBadGateway)
		return
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		upstream.Close()
		return
	}
	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	defer clientConn.Close()
	defer upstream.Close()

	// Send the 101 line + headers back to the client.
	if err := resp.Write(clientBuf); err != nil {
		logger.Printf("ws: write response: %v", err)
		return
	}
	if err := clientBuf.Flush(); err != nil {
		return
	}
	// Bidirectional copy. Any bytes the backend already buffered in `br`
	// must be flushed first.
	go func() {
		if n := br.Buffered(); n > 0 {
			b, _ := br.Peek(n)
			_, _ = clientConn.Write(b)
			_, _ = br.Discard(n)
		}
		_, _ = io.Copy(clientConn, upstream)
		if cw, ok := clientConn.(closeWriter); ok {
			_ = cw.CloseWrite()
		}
	}()
	_, _ = io.Copy(upstream, clientBuf)
	if cw, ok := upstream.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

type closeWriter interface {
	CloseWrite() error
}

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func stripHopByHop(h http.Header) {
	// The Connection header lists per-hop headers to strip.
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if name := strings.TrimSpace(f); name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
}

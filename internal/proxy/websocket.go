package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type closeWriter interface {
	CloseWrite() error
}

// proxyWebSocket tunnels an HTTP/1.1 Upgrade request to the upstream target.
// It dials the backend, replays the client's handshake there, relays the
// backend's answer, and then copies bytes in both directions until both
// sides are done.
func (s *Server) proxyWebSocket(w http.ResponseWriter, r *http.Request, target *url.URL, host string) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeDiag(w, http.StatusInternalServerError, "websocket unsupported", host,
			"This connection cannot be upgraded (HTTP/2 has no classic WebSocket handshake).",
			"Connect with HTTP/1.1.")
		return
	}
	upstream, err := net.DialTimeout("tcp", target.Host, 5*time.Second)
	if err != nil {
		writeDiag(w, http.StatusBadGateway, "upstream unreachable", host,
			fmt.Sprintf("Could not reach %s: %v", target.Host, err),
			"Is your dev server running on that port?")
		return
	}
	defer upstream.Close()

	out := r.Clone(r.Context())
	stripHopByHop(out.Header)
	out.Header.Set("Connection", "Upgrade")
	out.Header.Set("Upgrade", "websocket")
	for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded"} {
		out.Header.Del(h) // never trust client-supplied forwarding headers
	}
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		out.Header.Set("X-Forwarded-For", ip)
	}
	out.Header.Set("X-Forwarded-Host", r.Host)
	if r.TLS != nil {
		out.Header.Set("X-Forwarded-Proto", "https")
	} else {
		out.Header.Set("X-Forwarded-Proto", "http")
	}
	if s.opts.PreserveHost {
		out.Host = r.Host
	} else {
		out.Host = target.Host
	}

	_ = upstream.SetDeadline(time.Now().Add(15 * time.Second)) // handshake only
	if err := out.Write(upstream); err != nil {
		writeDiag(w, http.StatusBadGateway, "websocket handshake failed", host, err.Error(), "The upstream closed the connection.")
		return
	}
	br := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(br, out)
	if err != nil {
		writeDiag(w, http.StatusBadGateway, "websocket handshake failed", host, err.Error(), "The upstream did not answer the upgrade request.")
		return
	}
	_ = upstream.SetDeadline(time.Time{})

	if resp.StatusCode != http.StatusSwitchingProtocols {
		defer resp.Body.Close()
		copyHeader(w.Header(), resp.Header)
		stripHopByHop(w.Header())
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		writeDiag(w, http.StatusInternalServerError, "websocket unsupported", host, err.Error(), "Connect with HTTP/1.1.")
		return
	}
	defer clientConn.Close()
	_ = clientConn.SetDeadline(time.Time{})

	if err := resp.Write(clientBuf); err != nil {
		s.opts.Logger.Printf("ws: write 101 response: %v", err)
		return
	}
	if err := clientBuf.Flush(); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	pipe := func(dst net.Conn, src io.Reader) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(closeWriter); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(clientConn, br)        // upstream -> client (br also holds bytes read ahead)
	go pipe(upstream, clientBuf)   // client -> upstream
	<-done
	select { // let the other direction finish its close handshake, but not forever
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
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
	// The Connection header names additional per-hop headers to strip.
	for _, c := range h.Values("Connection") {
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

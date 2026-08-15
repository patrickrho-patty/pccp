package webbinding

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"

	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

// selfSignedCert delegates to the shared dari dev-cert helper.
func selfSignedCert() (*tls.Certificate, error) {
	cert, err := dari.DevSelfSignedCert("DARI web", []string{"localhost"})
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// WTServer binds a webbinding.Server to HTTP/3 + WebTransport.
type WTServer struct {
	WT    *webtransport.Server
	srv   *Server
	pconn *net.UDPConn
}

// NewWTServer builds the carrier. Addr like "127.0.0.1:0"; cert can be
// nil for a generated self-signed dev certificate.
func NewWTServer(srv *Server, addr string, cert *tls.Certificate) (*WTServer, error) {
	if cert == nil {
		generated, err := selfSignedCert()
		if err != nil {
			return nil, err
		}
		cert = generated
	}
	wt := &webtransport.Server{
		H3: &http3.Server{
			Addr: addr,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{*cert},
				NextProtos:   []string{"h3"},
			},
			EnableDatagrams: true,
		},
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Non-browser/native WT clients omit Origin; the origin
				// policy is still enforced at the OPEN proof.
				return true
			}
			return srv.VerifyWebOrigin(origin) == nil
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/dari.web/1", func(w http.ResponseWriter, r *http.Request) {
		// Serve the session SYNCHRONOUSLY: returning from the handler
		// cancels the request context (and with it the session).
		sess, err := wt.Upgrade(w, r)
		if err != nil {
			return
		}
		ctx := context.Background()
		if r.Context() != nil {
			ctx = r.Context()
		}
		stream, err := sess.AcceptStream(ctx)
		if err != nil {
			_ = sess.CloseWithError(0, "no session stream")
			return
		}
		defer stream.Close()
		done := make(chan struct{})
		defer close(done)
		go func() {
			// Drain any additional streams.
			for {
				if _, err := sess.AcceptStream(r.Context()); err != nil {
					return
				}
			}
		}()
		_ = srv.WTSession(stream, done)
	})
	wt.H3.Handler = mux
	webtransport.ConfigureHTTP3Server(wt.H3)
	return &WTServer{WT: wt, srv: srv}, nil
}

// ListenAndServe starts the HTTP/3 listener.
func (w *WTServer) ListenAndServe() error { return w.WT.ListenAndServe() }

// BoundAddr returns the listener's address (after ListenOn).
func (w *WTServer) BoundAddr() string {
	if w.pconn == nil {
		return w.WT.H3.Addr
	}
	return w.pconn.LocalAddr().String()
}

// ListenOn binds a UDP addr and serves HTTP/3 on it.
func (w *WTServer) ListenOn(addr string) error {
	ln, err := net.ListenUDP("udp", resolveUDPAddr(addr))
	if err != nil {
		return err
	}
	w.pconn = ln
	go func() { _ = w.WT.Serve(ln) }()
	return nil
}

func resolveUDPAddr(addr string) *net.UDPAddr {
	if addr == "" || strings.HasSuffix(addr, ":0") {
		return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	}
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	}
	return a
}

// Close shuts the listener down.
func (w *WTServer) Close() error { return w.WT.Close() }

package muxserver

import (
	"context"
	"errors"
	"github.com/hashicorp/yamux"
	"github.com/sourcegraph/conc"
	"go.uber.org/zap"
	"io"
	"net"
	"storj.io/drpc"
	"storj.io/drpc/drpcserver"
	"sync"
	"sync/atomic"
)

const (
	// RemoteAddrKey is the key for the remote address.
	RemoteAddrKey = "remote-addr"

	// LocalAddrKey is the key for the local address.
	LocalAddrKey = "local-addr"
)

var (
	ErrServerActive         = errors.New("server is already active")
	ErrServerListenerActive = errors.New("server listener is still active - close listener first")
)

// Server
//
//	DRPC server that handles multiplexed streams
type Server struct {
	srv    *drpcserver.Server
	wg     *conc.WaitGroup
	mu     *sync.Mutex
	active *atomic.Bool
	logger *zap.Logger
}

// New returns a new multiplexed Server that serves handler
func New(handler drpc.Handler) *Server {
	return NewWithOptions(handler, drpcserver.Options{})
}

// NewWithOptions
//
//	Same as New but passes options to DRPC
func NewWithOptions(handler drpc.Handler, opts drpcserver.Options) *Server {
	active := &atomic.Bool{}
	active.Store(false)
	return &Server{
		srv:    drpcserver.NewWithOptions(handler, opts),
		wg:     conc.NewWaitGroup(),
		mu:     &sync.Mutex{},
		active: active,
		logger: zap.NewNop(),
	}
}

// SetLogger sets the logger for the server
func (s *Server) SetLogger(logger *zap.Logger) {
	s.logger = logger
}

// Close
//
//	Closes the server waiting for all inflight requests to complete.
//	If Serve has been called, the listener that called server should be
//	closed before this function is called.
func (s *Server) Close() error {
	if s.active.Load() {
		return ErrServerListenerActive
	}
	s.wg.Wait()
	return nil
}

// Serve
//
//	Listens on the given listener and handles all multiplexed streams.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	// acquire lock to check if the server is active
	s.mu.Lock()

	// only permit server to be called once at a time
	if s.active.Load() {
		s.mu.Unlock()
		return ErrServerActive
	}

	// mark server as active and inactive on exit
	s.active.Store(true)
	defer s.active.Store(false)

	// release lock
	s.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.logger.Error("failed to accept connection", zap.Error(err))
			return err
		}

		sess, err := yamux.Server(conn, nil)
		if err != nil {
			s.logger.Error("failed to create yamux server", zap.Error(err))
			return err
		}

		s.logger.Info("new session established", zap.Stringer("remote_addr", conn.RemoteAddr()))

		s.wg.Go(func() {
			s.handleSession(ctx, sess)
		})
	}
}

func (s *Server) handleSession(ctx context.Context, sess *yamux.Session) {
	for {
		conn, err := sess.Accept()
		if errors.Is(err, io.EOF) {
			s.logger.Info("session closed on EOF")
			break
		} else if err != nil {
			s.logger.Error("failed to accept connection in session", zap.Error(err))
			continue
		}

		// retrieve the remote and local addresses and store them in the request context
		ctx = context.WithValue(ctx, RemoteAddrKey, conn.RemoteAddr())
		ctx = context.WithValue(ctx, LocalAddrKey, conn.LocalAddr())

		s.logger.Debug("serving new connection", zap.Stringer("remote_addr", conn.RemoteAddr()))

		s.wg.Go(func() {
			s.srv.ServeOne(ctx, conn)
		})
	}
}

// ServeOne
//
//	Serves a single set of rpcs on the provided transport.
func (s *Server) ServeOne(ctx context.Context, conn io.ReadWriteCloser) error {
	return s.srv.ServeOne(ctx, conn)
}

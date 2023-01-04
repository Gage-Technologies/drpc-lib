package muxconn

import (
	"context"
	"io"

	"github.com/hashicorp/yamux"
	"storj.io/drpc"
	"storj.io/drpc/drpcconn"
)

var _ drpc.Conn = &Conn{}

// Conn implements drpc.Conn using the yamux
// multiplexer to allow concurrent RPCs
type Conn struct {
	conn     io.ReadWriteCloser
	sess     *yamux.Session
	isClosed bool
	closed   chan struct{}
}

// New returns a new multiplexed DRPC connection
func New(conn io.ReadWriteCloser) (*Conn, error) {
	sess, err := yamux.Client(conn, nil)
	if err != nil {
		return nil, err
	}

	return &Conn{
		conn:   conn,
		sess:   sess,
		closed: make(chan struct{}),
	}, nil
}

// Close closes the multiplexer session
// and the underlying connection.
func (m *Conn) Close() error {
	if !m.isClosed {
		m.isClosed = true
		defer close(m.closed)

		err := m.sess.Close()
		if err != nil {
			m.conn.Close()
			return err
		}
		return m.conn.Close()
	}
	return nil
}

// Closed returns a channel that will be closed
// when the connection is closed
func (m *Conn) Closed() <-chan struct{} {
	return m.closed
}

// Invoke issues the rpc on the transport serializing in, waits for a response, and deserializes it into out.
func (m *Conn) Invoke(ctx context.Context, rpc string, enc drpc.Encoding, in, out drpc.Message) error {
	conn, err := m.sess.Open()
	if err != nil {
		return err
	}
	defer conn.Close()
	dconn := drpcconn.New(conn)
	defer dconn.Close()
	return dconn.Invoke(ctx, rpc, enc, in, out)
}

// NewStream begins a streaming rpc on the connection.
func (m *Conn) NewStream(ctx context.Context, rpc string, enc drpc.Encoding) (drpc.Stream, error) {
	conn, err := m.sess.Open()
	if err != nil {
		return nil, err
	}
	dconn := drpcconn.New(conn)

	go func() {
		<-dconn.Closed()
		conn.Close()
	}()

	s, err := dconn.NewStream(ctx, rpc, enc)
	if err != nil {
		return nil, err
	}

	go func() {
		<-s.Context().Done()
		dconn.Close()
	}()

	return s, nil
}

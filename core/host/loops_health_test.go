package host

import (
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/health"
)

type singleConnListener struct {
	conns chan net.Conn
}

func (l singleConnListener) Accept() (net.Conn, error) {
	conn, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return conn, nil
}

func (l singleConnListener) Close() error   { return nil }
func (l singleConnListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestHealthServerDropsAClientThatStallsItsHeaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := newHealthServer("", health.NewHealth(time.Unix(0, 0)))
		client, server := net.Pipe()
		conns := make(chan net.Conn, 1)
		conns <- server
		close(conns)
		go func() { logCleanup(srv.Serve(singleConnListener{conns: conns})) }()
		defer func() { logCleanup(client.Close()) }()

		if _, err := client.Write([]byte("GET /health HTTP/1.1\r\nHost: herrscher\r\n")); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := io.ReadAll(client); err != nil {
			t.Fatalf("the stalled request must end with the server closing the connection, got %v", err)
		}
		if srv.ReadHeaderTimeout != healthReadHeaderTimeout {
			t.Fatalf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, healthReadHeaderTimeout)
		}
	})
}

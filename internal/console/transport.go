package console

import (
	"net"
	"sync"
)

type boundedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}
type boundedConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func newBoundedListener(l net.Listener, limit int) *boundedListener {
	return &boundedListener{Listener: l, slots: make(chan struct{}, limit), done: make(chan struct{})}
}

func (c *boundedConn) Close() error { e := c.Conn.Close(); c.once.Do(c.release); return e }

// Close must release a blocked Accept, which is no longer inside the wrapped
// listener's Accept call and would otherwise never observe the close.
func (l *boundedListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

// Accept waits for a free slot BEFORE accepting the connection.
//
// The previous loop accepted every connection and immediately closed it when
// all slots were held, which gave the excess client a bare TCP close that is
// indistinguishable from "the service is down", and a local flood kept a hot
// accept/close spin running without ever answering. Waiting here lets the
// kernel backlog hold the excess instead: every connection that is accepted is
// either served or answered with a real HTTP response once a slot frees, and a
// closed listener releases the waiter at once.
func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	c, e := l.Listener.Accept()
	if e != nil {
		<-l.slots
		return nil, e
	}
	return &boundedConn{Conn: c, release: func() { <-l.slots }}, nil
}

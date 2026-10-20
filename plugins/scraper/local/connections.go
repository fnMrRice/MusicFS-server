package scraperlocal

import (
	"net"
	"sync"
)

type channelListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func (l *channelListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	connection, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &channelConnection{Conn: connection, release: func() { <-l.slots }}, nil
}
func (l *channelListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type channelConnection struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *channelConnection) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }

package sandboxagent

import (
	"fmt"
	"io"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

type vsockAddr struct {
	cid  uint32
	port uint32
}

func (a vsockAddr) Network() string {
	return "vsock"
}

func (a vsockAddr) String() string {
	return fmt.Sprintf("%d:%d", a.cid, a.port)
}

type vsockListener struct {
	fd   int
	addr vsockAddr
}

func (l *vsockListener) Accept() (net.Conn, error) {
	for {
		fd, sa, err := unix.Accept4(l.fd, unix.SOCK_CLOEXEC)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		remote := vsockAddr{}
		if vm, ok := sa.(*unix.SockaddrVM); ok {
			remote = vsockAddr{cid: vm.CID, port: vm.Port}
		}
		return &vsockConn{fd: fd, local: l.addr, remote: remote}, nil
	}
}

func (l *vsockListener) Close() error {
	return unix.Close(l.fd)
}

func (l *vsockListener) Addr() net.Addr {
	return l.addr
}

type vsockConn struct {
	fd     int
	local  vsockAddr
	remote vsockAddr
}

func (c *vsockConn) Read(p []byte) (int, error) {
	n, err := unix.Read(c.fd, p)
	if err != nil {
		return n, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (c *vsockConn) Write(p []byte) (int, error) {
	return unix.Write(c.fd, p)
}

func (c *vsockConn) Close() error {
	return unix.Close(c.fd)
}

func (c *vsockConn) LocalAddr() net.Addr {
	return c.local
}

func (c *vsockConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *vsockConn) SetDeadline(_ time.Time) error {
	return nil
}

func (c *vsockConn) SetReadDeadline(_ time.Time) error {
	return nil
}

func (c *vsockConn) SetWriteDeadline(_ time.Time) error {
	return nil
}

func ListenVsock(port uint32) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("create vsock listener: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind vsock listener: %w", err)
	}
	if err := unix.Listen(fd, unix.SOMAXCONN); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("listen on vsock: %w", err)
	}
	return &vsockListener{fd: fd, addr: vsockAddr{cid: unix.VMADDR_CID_ANY, port: port}}, nil
}

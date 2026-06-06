//go:build linux

package eventloop

import (
	"golang.org/x/sys/unix"
)

const (
	maxEvents = 128
)

// epollPoller implements poller using Linux epoll.
type epollPoller struct {
	epfd int
}

// newPoller creates a new epoll-based poller.
func newPoller() (poller, error) {
	epfd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}
	return &epollPoller{epfd: epfd}, nil
}

func (p *epollPoller) addRead(fd int) error {
	return unix.EpollCtl(p.epfd, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(fd),
	})
}

func (p *epollPoller) modReadWrite(fd int) error {
	return unix.EpollCtl(p.epfd, unix.EPOLL_CTL_MOD, fd, &unix.EpollEvent{
		Events: unix.EPOLLIN | unix.EPOLLOUT,
		Fd:     int32(fd),
	})
}

func (p *epollPoller) remove(fd int) error {
	return unix.EpollCtl(p.epfd, unix.EPOLL_CTL_DEL, fd, nil)
}

func (p *epollPoller) wait(events []event, msec int) (int, error) {
	epEvents := make([]unix.EpollEvent, len(events))
	n, err := unix.EpollWait(p.epfd, epEvents, msec)
	if err != nil {
		if err == unix.EINTR {
			return 0, nil
		}
		return 0, err
	}
	for i := 0; i < n; i++ {
		events[i].fd = int(epEvents[i].Fd)
		events[i].readable = epEvents[i].Events&unix.EPOLLIN != 0
		events[i].writable = epEvents[i].Events&unix.EPOLLOUT != 0
	}
	return n, nil
}

func (p *epollPoller) close() error {
	return unix.Close(p.epfd)
}

//go:build darwin

package eventloop

import (
	"golang.org/x/sys/unix"
)

const (
	maxEvents = 128
)

// kqueuePoller implements poller using BSD kqueue.
type kqueuePoller struct {
	kq int
}

// newPoller creates a new kqueue-based poller.
func newPoller() (poller, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	return &kqueuePoller{kq: kq}, nil
}

func (p *kqueuePoller) addRead(fd int) error {
	changes := []unix.Kevent_t{{
		Ident:  uint64(fd),
		Filter: unix.EVFILT_READ,
		Flags:  unix.EV_ADD | unix.EV_ENABLE,
	}}
	_, err := unix.Kevent(p.kq, changes, nil, nil)
	return err
}

func (p *kqueuePoller) modReadWrite(fd int) error {
	changes := []unix.Kevent_t{
		{
			Ident:  uint64(fd),
			Filter: unix.EVFILT_READ,
			Flags:  unix.EV_ADD | unix.EV_ENABLE,
		},
		{
			Ident:  uint64(fd),
			Filter: unix.EVFILT_WRITE,
			Flags:  unix.EV_ADD | unix.EV_ENABLE,
		},
	}
	_, err := unix.Kevent(p.kq, changes, nil, nil)
	return err
}

func (p *kqueuePoller) remove(fd int) error {
	changes := []unix.Kevent_t{
		{
			Ident:  uint64(fd),
			Filter: unix.EVFILT_READ,
			Flags:  unix.EV_DELETE,
		},
		{
			Ident:  uint64(fd),
			Filter: unix.EVFILT_WRITE,
			Flags:  unix.EV_DELETE,
		},
	}
	// Ignore errors — fd may only have read-interest.
	_, _ = unix.Kevent(p.kq, changes, nil, nil)
	return nil
}

func (p *kqueuePoller) wait(events []event, msec int) (int, error) {
	var timeout *unix.Timespec
	if msec >= 0 {
		ts := unix.NsecToTimespec(int64(msec) * 1e6)
		timeout = &ts
	}
	kevents := make([]unix.Kevent_t, len(events))
	n, err := unix.Kevent(p.kq, nil, kevents, timeout)
	if err != nil {
		if err == unix.EINTR {
			return 0, nil
		}
		return 0, err
	}
	for i := 0; i < n; i++ {
		events[i].fd = int(kevents[i].Ident)
		if kevents[i].Filter == unix.EVFILT_READ {
			events[i].readable = true
		}
		if kevents[i].Filter == unix.EVFILT_WRITE {
			events[i].writable = true
		}
	}
	return n, nil
}

func (p *kqueuePoller) close() error {
	return unix.Close(p.kq)
}

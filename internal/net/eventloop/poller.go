package eventloop

// event represents a readiness event for a file descriptor.
type event struct {
	fd       int
	readable bool
	writable bool
}

// poller is the platform-specific I/O multiplexer abstraction.
type poller interface {
	// addRead registers fd for read-interest.
	addRead(fd int) error

	// modReadWrite changes fd to monitor both read and write readiness.
	modReadWrite(fd int) error

	// remove deregisters fd from the poller.
	remove(fd int) error

	// wait blocks until at least one event is ready or msec elapses.
	// Returns the number of events stored in events.
	wait(events []event, msec int) (int, error)

	// close releases the poller's resources.
	close() error
}

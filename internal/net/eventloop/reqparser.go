package eventloop

import (
	"bytes"
	"errors"
)

var (
	errProtocolError = errors.New("protocol error")
	errIncomplete    = errors.New("incomplete request")
)

// parseUint parses a non-negative integer from buf without allocating.
// Returns the value and true on success, or (0, false) on overflow/invalid.
// Rejects values that would overflow int (>= 1<<63 on 64-bit platforms).
func parseUint(buf []byte) (int, bool) {
	if len(buf) == 0 {
		return 0, false
	}
	const maxInt = int(^uint(0) >> 1) // max value for int
	n := 0
	for _, b := range buf {
		if b < '0' || b > '9' {
			return 0, false
		}
		digit := int(b - '0')
		// Check overflow before it happens: n*10+digit > maxInt
		if n > (maxInt-digit)/10 {
			return 0, false
		}
		n = n*10 + digit
	}
	return n, true
}

// parseRequests parses as many complete RESP2 multibulk commands as possible
// from buf. It returns the parsed commands (each as [][]byte of arguments),
// the number of bytes consumed from buf, and any error.
// If the buffer ends mid-command, it returns errIncomplete.
func parseRequests(buf []byte) ([][][]byte, int, error) {
	cmds := make([][][]byte, 0, 8) // pre-allocate for typical pipeline depth
	offset := 0
	for offset < len(buf) {
		cmd, consumed, err := parseOneMultibulk(buf[offset:])
		if err == errIncomplete {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		cmds = append(cmds, cmd)
		offset += consumed
	}
	return cmds, offset, nil
}

// parseOneMultibulk parses a single RESP2 multibulk command from buf.
// Returns the command args, bytes consumed, and error.
func parseOneMultibulk(buf []byte) ([][]byte, int, error) {
	if len(buf) == 0 {
		return nil, 0, errIncomplete
	}
	if buf[0] != '*' {
		return nil, 0, errProtocolError
	}

	// Skip the '*' prefix and read the count line.
	line, lineLen, err := readLine(buf[1:])
	if err != nil {
		return nil, 0, err
	}
	count, ok := parseUint(line)
	if !ok {
		return nil, 0, errProtocolError
	}
	// Account for the '*' we skipped.
	lineLen++

	consumed := lineLen
	args := make([][]byte, count)
	for i := 0; i < count; i++ {
		if consumed >= len(buf) {
			return nil, 0, errIncomplete
		}
		if buf[consumed] != '$' {
			return nil, 0, errProtocolError
		}
		// Skip the '$' prefix and read the length line.
		argLine, argLineLen, err := readLine(buf[consumed+1:])
		if err != nil {
			return nil, 0, err
		}
		argLen := 0
		if len(argLine) > 0 && argLine[0] == '-' {
			// Negative length: only -1 (null bulk) is valid.
			if string(argLine) != "-1" {
				return nil, 0, errProtocolError
			}
			argLen = -1
		} else {
			var ok bool
			argLen, ok = parseUint(argLine)
			if !ok {
				return nil, 0, errProtocolError
			}
		}
		// Account for the '$' we skipped.
		consumed += argLineLen + 1
		if argLen == -1 {
			args[i] = nil
			continue
		}
		// need argLen bytes + \r\n
		if consumed+argLen+2 > len(buf) {
			return nil, 0, errIncomplete
		}
		args[i] = buf[consumed : consumed+argLen]
		consumed += argLen + 2 // skip arg data + \r\n
	}
	return args, consumed, nil
}

// readLine reads up to the first \r\n in buf.
// Returns the line content (without \r\n), the total bytes consumed (including \r\n),
// and errIncomplete if no \r\n is found.
func readLine(buf []byte) ([]byte, int, error) {
	idx := bytes.Index(buf, []byte("\r\n"))
	if idx < 0 {
		return nil, 0, errIncomplete
	}
	return buf[:idx], idx + 2, nil
}

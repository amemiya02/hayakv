package eventloop

import (
	"bytes"
	"errors"
	"strconv"
)

var (
	errProtocolError = errors.New("protocol error")
	errIncomplete    = errors.New("incomplete request")
)

// parseRequests parses as many complete RESP2 multibulk commands as possible
// from buf. It returns the parsed commands (each as [][]byte of arguments),
// the number of bytes consumed from buf, and any error.
// If the buffer ends mid-command, it returns errIncomplete.
func parseRequests(buf []byte) ([][][]byte, int, error) {
	var cmds [][][]byte
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
	count, err := strconv.Atoi(string(line))
	if err != nil || count < 0 {
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
		argLen, err := strconv.Atoi(string(argLine))
		if err != nil || argLen < -1 {
			return nil, 0, errProtocolError
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

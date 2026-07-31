package transportprimitives

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ErrFrameLimit is the sentinel for frame-size violations.
var ErrFrameLimit = errors.New("frame size limit exceeded")

// WriteFrame writes a length-prefixed (big-endian uint32) frame to writer.
// The payload must be non-empty and within limit.
func WriteFrame(writer io.Writer, payload []byte, limit uint32) error {
	if len(payload) == 0 || uint64(len(payload)) > uint64(limit) {
		return fmt.Errorf("%w: outbound frame size", ErrFrameLimit)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeBytes(writer, header[:]); err != nil {
		return err
	}
	return writeBytes(writer, payload)
}

// ReadFrame reads a length-prefixed (big-endian uint32) frame from reader.
// The frame length must be non-zero and within limit.
func ReadFrame(reader io.Reader, limit uint32) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > limit {
		return nil, fmt.Errorf("%w: inbound frame size", ErrFrameLimit)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeBytes(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		value = value[written:]
	}
	return nil
}

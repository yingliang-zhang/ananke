package trustedsupervisor

import (
	"errors"
	"fmt"
	"io"

	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

func writeFrame(writer io.Writer, payload []byte, limit uint32) error {
	err := transportprimitives.WriteFrame(writer, payload, limit)
	if err == nil {
		return nil
	}
	if errors.Is(err, transportprimitives.ErrFrameLimit) {
		return fmt.Errorf("%w: %v", ErrLimit, err)
	}
	return err
}

func readFrame(reader io.Reader, limit uint32) ([]byte, error) {
	payload, err := transportprimitives.ReadFrame(reader, limit)
	if err == nil {
		return payload, nil
	}
	if errors.Is(err, transportprimitives.ErrFrameLimit) {
		return nil, fmt.Errorf("%w: %v", ErrLimit, err)
	}
	return nil, err
}

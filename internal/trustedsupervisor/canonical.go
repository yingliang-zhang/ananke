package trustedsupervisor

import (
	"bytes"
	"fmt"

	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

func marshalCanonical(value any) ([]byte, error) {
	return transportprimitives.MarshalCanonical(value)
}

func decodeCanonical(data []byte, destination any) error {
	if err := transportprimitives.DecodeCanonical(data, destination); err != nil {
		return fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	return nil
}

func decodeJSONValue(data []byte) (any, error) {
	return transportprimitives.DecodeJSONValue(data)
}

func appendCanonicalValue(output *bytes.Buffer, value any) error {
	return transportprimitives.AppendCanonicalValue(output, value)
}

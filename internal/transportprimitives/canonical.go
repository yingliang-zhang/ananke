package transportprimitives

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrCanonical is the sentinel for canonical-JSON violations.
var ErrCanonical = errors.New("canonical JSON error")

// MarshalCanonical encodes value as RFC 8785/JCS canonical JSON.
func MarshalCanonical(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	normalized, err := decodeJSONValue(encoded)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := appendCanonicalValue(&output, normalized); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// DecodeCanonical requires canonical JSON and a closed schema
// (DisallowUnknownFields). It returns ErrCanonical-wrapped errors so
// callers can re-wrap with their own package-specific sentinels.
func DecodeCanonical(data []byte, destination any) error {
	normalized, err := decodeJSONValue(data)
	if err != nil {
		return fmt.Errorf("%w: invalid JSON: %v", ErrCanonical, err)
	}
	var canonical bytes.Buffer
	if err := appendCanonicalValue(&canonical, normalized); err != nil {
		return fmt.Errorf("%w: invalid canonical value: %v", ErrCanonical, err)
	}
	if !bytes.Equal(data, canonical.Bytes()) {
		return fmt.Errorf("%w: noncanonical JSON", ErrCanonical)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: closed schema: %v", ErrCanonical, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return fmt.Errorf("%w: trailing JSON", ErrCanonical)
	}
	return nil
}

// CanonicalHash returns "sha256:" + hex-encoded SHA-256 of the canonical
// JSON encoding of value.
func CanonicalHash(value any) (string, error) {
	canonical, err := MarshalCanonical(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// DecodeJSONValue decodes a single JSON value using json.Number for
// precise number handling. Exported for consumers that need the
// intermediate decoded representation before canonical re-encoding.
func DecodeJSONValue(data []byte) (any, error) {
	return decodeJSONValue(data)
}

// AppendCanonicalValue appends the canonical encoding of a pre-decoded
// JSON value to output. Exported for consumers that need to canonicalize
// an already-decoded value without a full marshal/decode round-trip.
func AppendCanonicalValue(output *bytes.Buffer, value any) error {
	return appendCanonicalValue(output, value)
}

func decodeJSONValue(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errorsIsEOF(err) {
		if err == nil {
			return errorsNewTrailingJSON()
		}
		return err
	}
	return nil
}

func errorsIsEOF(err error) bool   { return err == io.EOF }
func errorsNewTrailingJSON() error { return fmt.Errorf("multiple JSON values") }

func appendCanonicalValue(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		return appendCanonicalString(output, typed)
	case json.Number:
		number, err := canonicalNumber(string(typed))
		if err != nil {
			return err
		}
		output.WriteString(number)
	case []any:
		output.WriteByte('[')
		for index := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalValue(output, typed[index]); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool { return compareUTF16(keys[left], keys[right]) < 0 })
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalString(output, key); err != nil {
				return err
			}
			output.WriteByte(':')
			if err := appendCanonicalValue(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON value %T", value)
	}
	return nil
}

func appendCanonicalString(output *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("canonical JSON string is not valid UTF-8")
	}
	for _, runeValue := range value {
		if runeValue >= 0xD800 && runeValue <= 0xDFFF {
			return fmt.Errorf("canonical JSON string has an unpaired Unicode surrogate")
		}
	}
	output.WriteByte('"')
	for _, runeValue := range value {
		switch runeValue {
		case '"':
			output.WriteString(`\"`)
		case '\\':
			output.WriteString(`\\`)
		case '\b':
			output.WriteString(`\b`)
		case '\f':
			output.WriteString(`\f`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		default:
			if runeValue < 0x20 {
				output.WriteString(`\u00`)
				output.WriteByte("0123456789abcdef"[runeValue>>4])
				output.WriteByte("0123456789abcdef"[runeValue&0x0f])
				continue
			}
			output.WriteRune(runeValue)
		}
	}
	output.WriteByte('"')
	return nil
}

func compareUTF16(left, right string) int {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	for index := 0; index < len(leftUnits) && index < len(rightUnits); index++ {
		if leftUnits[index] < rightUnits[index] {
			return -1
		}
		if leftUnits[index] > rightUnits[index] {
			return 1
		}
	}
	switch {
	case len(leftUnits) < len(rightUnits):
		return -1
	case len(leftUnits) > len(rightUnits):
		return 1
	default:
		return 0
	}
}

func canonicalNumber(raw string) (string, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("canonical JSON requires a finite number %q", raw)
	}
	if value == 0 {
		return "0", nil
	}
	formatted := strconv.FormatFloat(value, 'g', -1, 64)
	exponentAt := strings.IndexByte(formatted, 'e')
	if exponentAt == -1 {
		return formatted, nil
	}
	mantissa, exponentText := formatted[:exponentAt], formatted[exponentAt+1:]
	exponent, err := strconv.Atoi(exponentText)
	if err != nil {
		return "", err
	}
	if exponent >= -6 && exponent < 21 {
		return expandDecimal(mantissa, exponent), nil
	}
	if exponent >= 0 {
		return mantissa + "e+" + strconv.Itoa(exponent), nil
	}
	return mantissa + "e" + strconv.Itoa(exponent), nil
}

func expandDecimal(mantissa string, exponent int) string {
	sign := ""
	if mantissa[0] == '-' {
		sign, mantissa = "-", mantissa[1:]
	}
	decimalAt := strings.IndexByte(mantissa, '.')
	if decimalAt == -1 {
		decimalAt = len(mantissa)
	}
	digits := strings.ReplaceAll(mantissa, ".", "")
	newDecimalAt := decimalAt + exponent
	switch {
	case newDecimalAt <= 0:
		return sign + "0." + strings.Repeat("0", -newDecimalAt) + digits
	case newDecimalAt >= len(digits):
		return sign + digits + strings.Repeat("0", newDecimalAt-len(digits))
	default:
		return sign + digits[:newDecimalAt] + "." + digits[newDecimalAt:]
	}
}

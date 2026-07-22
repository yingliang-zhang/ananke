package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// canonicalJSONHash returns the P1a sha256: digest of RFC 8785 JCS bytes.
// P1a only permits strings, positive integer revisions, arrays, nulls, and
// fixed-object policy values, but this encoder also canonicalizes JSON numbers
// so request hashing does not depend on a Go JSON encoder's formatting choices.
func canonicalJSONHash(value any) (string, error) {
	canonical, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + fmt.Sprintf("%x", digest[:]), nil
}

func canonicalJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := appendCanonicalJSON(&output, reflect.ValueOf(value)); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendCanonicalJSON(output *bytes.Buffer, value reflect.Value) error {
	if !value.IsValid() {
		output.WriteString("null")
		return nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			output.WriteString("null")
			return nil
		}
		return appendCanonicalJSON(output, value.Elem())
	}

	switch value.Kind() {
	case reflect.Bool:
		if value.Bool() {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
		return nil
	case reflect.String:
		if value.Type() == reflect.TypeFor[json.Number]() {
			number, err := canonicalJSONNumber(value.String())
			if err != nil {
				return err
			}
			output.WriteString(number)
			return nil
		}
		return appendCanonicalJSONString(output, value.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		output.WriteString(strconv.FormatInt(value.Int(), 10))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		output.WriteString(strconv.FormatUint(value.Uint(), 10))
		return nil
	case reflect.Float32, reflect.Float64:
		number, err := canonicalJSONNumber(strconv.FormatFloat(value.Float(), 'g', -1, value.Type().Bits()))
		if err != nil {
			return err
		}
		output.WriteString(number)
		return nil
	case reflect.Slice, reflect.Array:
		output.WriteByte('[')
		for index := 0; index < value.Len(); index++ {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalJSON(output, value.Index(index)); err != nil {
				return err
			}
		}
		output.WriteByte(']')
		return nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("canonical JSON object key must be string, got %s", value.Type().Key())
		}
		keys := value.MapKeys()
		sort.Slice(keys, func(left, right int) bool {
			return compareUTF16(keys[left].String(), keys[right].String()) < 0
		})
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalJSONString(output, key.String()); err != nil {
				return err
			}
			output.WriteByte(':')
			if err := appendCanonicalJSON(output, value.MapIndex(key)); err != nil {
				return err
			}
		}
		output.WriteByte('}')
		return nil
	default:
		return fmt.Errorf("unsupported canonical JSON value %s", value.Type())
	}
}

func appendCanonicalJSONString(output *bytes.Buffer, value string) error {
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

func canonicalJSONNumber(raw string) (string, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("canonical JSON requires a finite JSON number %q", raw)
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
		return "", fmt.Errorf("parse canonical exponent %q: %w", formatted, err)
	}
	if exponent >= -6 && exponent < 21 {
		return expandCanonicalDecimal(mantissa, exponent), nil
	}
	return mantissa + "e" + canonicalExponent(exponent), nil
}

func canonicalExponent(exponent int) string {
	if exponent >= 0 {
		return "+" + strconv.Itoa(exponent)
	}
	return strconv.Itoa(exponent)
}

func expandCanonicalDecimal(mantissa string, exponent int) string {
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

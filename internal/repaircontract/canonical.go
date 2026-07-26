package repaircontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func sha256Digest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func canonicalBytes(value any) ([]byte, error) {
	if err := validateGoStrings(reflect.ValueOf(value)); err != nil {
		return nil, ErrInvalidContract
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidContract
	}
	normalized, err := decodeUniqueJSON(encoded)
	if err != nil {
		return nil, ErrInvalidContract
	}
	var output bytes.Buffer
	if err := appendCanonicalJSON(&output, reflect.ValueOf(normalized)); err != nil {
		return nil, ErrInvalidContract
	}
	return output.Bytes(), nil
}

func hashRecord(value any, ownHashField string) (string, error) {
	if ownHashField == "" {
		return "", ErrInvalidContract
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalidContract
	}
	normalized, err := decodeUniqueJSON(encoded)
	if err != nil {
		return "", ErrInvalidContract
	}
	object, ok := normalized.(map[string]any)
	if !ok {
		return "", ErrInvalidContract
	}
	if _, exists := object[ownHashField]; !exists {
		return "", ErrInvalidContract
	}
	delete(object, ownHashField)
	canonical, err := canonicalBytes(object)
	if err != nil {
		return "", ErrInvalidContract
	}
	return sha256Digest(canonical), nil
}

// DecodeFixture accepts only one canonical, closed, duplicate-free JSON value.
// Diagnostics intentionally collapse to ErrInvalidContract so an unknown
// secret-looking member or its value can never be reflected to a caller.
func DecodeFixture(raw []byte) (ContractFixture, error) {
	return decodeCanonicalRecord[ContractFixture](raw)
}

func decodeCanonicalRecord[T any](raw []byte) (T, error) {
	var zero T
	if !utf8.Valid(raw) || len(raw) == 0 || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return zero, ErrInvalidContract
	}
	if err := validateRawJSONStringScalars(raw); err != nil {
		return zero, ErrInvalidContract
	}
	normalized, err := decodeUniqueJSON(raw)
	if err != nil {
		return zero, ErrInvalidContract
	}
	canonical, err := canonicalBytes(normalized)
	if err != nil || !bytes.Equal(raw, canonical) {
		return zero, ErrInvalidContract
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return zero, ErrInvalidContract
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return zero, ErrInvalidContract
	}
	return value, nil
}

func decodeUniqueJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeUniqueValue(decoder)
	if err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return nil, ErrInvalidContract
	}
	return value, nil
}

func decodeUniqueValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		switch value := token.(type) {
		case nil, bool, json.Number:
			return value, nil
		case string:
			if !validUnicodeScalars(value) {
				return nil, ErrInvalidContract
			}
			return value, nil
		default:
			return nil, ErrInvalidContract
		}
	}

	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok || !validUnicodeScalars(key) {
				return nil, ErrInvalidContract
			}
			if _, duplicate := object[key]; duplicate {
				return nil, ErrInvalidContract
			}
			value, err := decodeUniqueValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return nil, ErrInvalidContract
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeUniqueValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return nil, ErrInvalidContract
		}
		return array, nil
	default:
		return nil, ErrInvalidContract
	}
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
		for index := range value.Len() {
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
			return fmt.Errorf("canonical JSON object key must be a string")
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
		return fmt.Errorf("unsupported canonical JSON value")
	}
}

func appendCanonicalJSONString(output *bytes.Buffer, value string) error {
	if !validUnicodeScalars(value) {
		return ErrInvalidContract
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

func canonicalJSONNumber(raw string) (string, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", ErrInvalidContract
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
		return "", ErrInvalidContract
	}
	if exponent >= -6 && exponent < 21 {
		return expandCanonicalDecimal(mantissa, exponent), nil
	}
	if exponent >= 0 {
		return mantissa + "e+" + strconv.Itoa(exponent), nil
	}
	return mantissa + "e" + strconv.Itoa(exponent), nil
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

func compareUTF16(left, right string) int {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	for index := range min(len(leftUnits), len(rightUnits)) {
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

func validateGoStrings(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return validateGoStrings(value.Elem())
	}
	switch value.Kind() {
	case reflect.String:
		if value.Type() != reflect.TypeFor[json.Number]() && !validUnicodeScalars(value.String()) {
			return ErrInvalidContract
		}
	case reflect.Struct:
		for index := range value.NumField() {
			if err := validateGoStrings(value.Field(index)); err != nil {
				return err
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateGoStrings(iterator.Key()); err != nil {
				return err
			}
			if err := validateGoStrings(iterator.Value()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			if err := validateGoStrings(value.Index(index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validUnicodeScalars(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, runeValue := range value {
		if runeValue >= 0xd800 && runeValue <= 0xdfff || isUnicodeNoncharacter(runeValue) {
			return false
		}
	}
	return true
}

func isUnicodeNoncharacter(value rune) bool {
	return value >= 0xfdd0 && value <= 0xfdef || value >= 0 && value <= utf8.MaxRune && value&0xffff >= 0xfffe
}

// validateRawJSONStringScalars catches lone escaped UTF-16 surrogates before
// encoding/json can replace them with U+FFFD. JSON syntax itself is left to the
// decoder; this pass only examines quoted scalar content.
func validateRawJSONStringScalars(raw []byte) error {
	for index := 0; index < len(raw); {
		if raw[index] != '"' {
			index++
			continue
		}
		index++
		for index < len(raw) && raw[index] != '"' {
			if raw[index] < 0x20 {
				return ErrInvalidContract
			}
			if raw[index] != '\\' {
				runeValue, size := utf8.DecodeRune(raw[index:])
				if runeValue == utf8.RuneError && size == 1 || isUnicodeNoncharacter(runeValue) {
					return ErrInvalidContract
				}
				index += size
				continue
			}
			index++
			if index >= len(raw) {
				return ErrInvalidContract
			}
			if raw[index] != 'u' {
				if !strings.ContainsRune(`"\\/bfnrt`, rune(raw[index])) {
					return ErrInvalidContract
				}
				index++
				continue
			}
			first, next, ok := readHexCodeUnit(raw, index+1)
			if !ok {
				return ErrInvalidContract
			}
			index = next
			switch {
			case first >= 0xd800 && first <= 0xdbff:
				if index+2 > len(raw) || raw[index] != '\\' || raw[index+1] != 'u' {
					return ErrInvalidContract
				}
				second, afterSecond, ok := readHexCodeUnit(raw, index+2)
				if !ok || second < 0xdc00 || second > 0xdfff {
					return ErrInvalidContract
				}
				combined := utf16.DecodeRune(rune(first), rune(second))
				if isUnicodeNoncharacter(combined) {
					return ErrInvalidContract
				}
				index = afterSecond
			case first >= 0xdc00 && first <= 0xdfff:
				return ErrInvalidContract
			case isUnicodeNoncharacter(rune(first)):
				return ErrInvalidContract
			}
		}
		if index >= len(raw) || raw[index] != '"' {
			return ErrInvalidContract
		}
		index++
	}
	return nil
}

func readHexCodeUnit(raw []byte, start int) (uint16, int, bool) {
	if start+4 > len(raw) {
		return 0, start, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint16(character-'A') + 10
		default:
			return 0, start, false
		}
	}
	return value, start + 4, true
}

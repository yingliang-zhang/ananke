package transportprimitives

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestMarshalCanonicalBasicValues(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"null", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"empty_string", "", `""`},
		{"hello", "hello", `"hello"`},
		{"integer", 42, "42"},
		{"zero", 0, "0"},
		{"negative", -1, "-1"},
		{"float", 1.5, "1.5"},
		{"empty_array", []any{}, "[]"},
		{"array", []any{1, 2, 3}, "[1,2,3]"},
		{"empty_object", map[string]any{}, "{}"},
		{"object", map[string]any{"b": 1, "a": 2}, `{"a":2,"b":1}`},
		{"escaped_chars", "a\nb\t", `"a\nb\t"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MarshalCanonical(tc.input)
			if err != nil {
				t.Fatalf("MarshalCanonical(%v) error: %v", tc.input, err)
			}
			if string(got) != tc.want {
				t.Errorf("MarshalCanonical(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDecodeCanonicalRejectsNonCanonical(t *testing.T) {
	nonCanonical := []byte(`{"b":1,"a":2}`)
	var dest map[string]any
	if err := DecodeCanonical(nonCanonical, &dest); err == nil {
		t.Fatal("DecodeCanonical accepted non-canonical key order")
	}
}

func TestDecodeCanonicalAcceptsCanonical(t *testing.T) {
	canonical := []byte(`{"a":2,"b":1}`)
	type dest struct {
		A int `json:"a"`
		B int `json:"b"`
	}
	var d dest
	if err := DecodeCanonical(canonical, &d); err != nil {
		t.Fatalf("DecodeCanonical rejected canonical input: %v", err)
	}
	if d.A != 2 || d.B != 1 {
		t.Errorf("decoded = %+v, want A=2 B=1", d)
	}
}

func TestDecodeCanonicalRejectsUnknownFields(t *testing.T) {
	type strict struct {
		A int `json:"a"`
	}
	canonical := []byte(`{"a":1,"b":2}`)
	var dest strict
	if err := DecodeCanonical(canonical, &dest); err == nil {
		t.Fatal("DecodeCanonical accepted unknown field")
	}
}

func TestCanonicalHashStability(t *testing.T) {
	hash1, err := CanonicalHash(map[string]any{"key": "value", "num": 42})
	if err != nil {
		t.Fatalf("CanonicalHash error: %v", err)
	}
	hash2, err := CanonicalHash(map[string]any{"num": 42, "key": "value"})
	if err != nil {
		t.Fatalf("CanonicalHash error: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("CanonicalHash not order-independent: %q vs %q", hash1, hash2)
	}
	if !strings.HasPrefix(hash1, "sha256:") {
		t.Errorf("CanonicalHash prefix = %q, want sha256:", hash1[:7])
	}
}

func TestDecodeJSONValueRoundTrip(t *testing.T) {
	input := []byte(`{"a":1,"b":[2,3],"c":"hello"}`)
	decoded, err := DecodeJSONValue(input)
	if err != nil {
		t.Fatalf("DecodeJSONValue error: %v", err)
	}
	var buf bytes.Buffer
	if err := AppendCanonicalValue(&buf, decoded); err != nil {
		t.Fatalf("AppendCanonicalValue error: %v", err)
	}
	expected := `{"a":1,"b":[2,3],"c":"hello"}`
	if buf.String() != expected {
		t.Errorf("round-trip = %q, want %q", buf.String(), expected)
	}
}

func TestSPKIHashAndParsePublicKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey error: %v", err)
	}
	hash, err := SPKIHash(pub)
	if err != nil {
		t.Fatalf("SPKIHash error: %v", err)
	}
	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("SPKIHash prefix = %q, want sha256:", hash[:7])
	}
	encoded := "ed25519:" + hex.EncodeToString(pub)
	parsed, err := ParsePublicKey(encoded, hash)
	if err != nil {
		t.Fatalf("ParsePublicKey error: %v", err)
	}
	if !bytes.Equal(pub, parsed) {
		t.Error("ParsePublicKey returned different key")
	}
}

func TestParsePublicKeyRejectsMismatch(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	wrongHash := "sha256:" + strings.Repeat("0", 64)
	encoded := "ed25519:" + hex.EncodeToString(pub)
	if _, err := ParsePublicKey(encoded, wrongHash); err == nil {
		t.Fatal("ParsePublicKey accepted mismatched SPKI")
	}
}

func TestDecodePrefixedHex(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	encoded := "ed25519:" + hex.EncodeToString(data)
	decoded, err := DecodePrefixedHex(encoded, "ed25519:", 4)
	if err != nil {
		t.Fatalf("DecodePrefixedHex error: %v", err)
	}
	if !bytes.Equal(data, decoded) {
		t.Errorf("DecodePrefixedHex = %v, want %v", decoded, data)
	}
}

func TestDecodePrefixedHexRejectsWrongSize(t *testing.T) {
	encoded := "ed25519:" + hex.EncodeToString([]byte{0x01})
	if _, err := DecodePrefixedHex(encoded, "ed25519:", 32); err == nil {
		t.Fatal("DecodePrefixedHex accepted wrong size")
	}
}

func TestRecordValidAt(t *testing.T) {
	issuedAt := "2026-01-01T00:00:00Z"
	notAfter := "2026-12-31T23:59:59Z"
	if !RecordValidAt(issuedAt, notAfter, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("RecordValidAt should be true for mid-range time")
	}
	if RecordValidAt(issuedAt, notAfter, time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("RecordValidAt should be false for before issuedAt")
	}
	if RecordValidAt(issuedAt, notAfter, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("RecordValidAt should be false for after notAfter")
	}
}

func TestWriteFrameReadFrameRoundTrip(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	var buf bytes.Buffer
	if err := WriteFrame(&buf, payload, 64*1024); err != nil {
		t.Fatalf("WriteFrame error: %v", err)
	}
	got, err := ReadFrame(&buf, 64*1024)
	if err != nil {
		t.Fatalf("ReadFrame error: %v", err)
	}
	if !bytes.Equal(payload, got) {
		t.Errorf("round-trip = %q, want %q", got, payload)
	}
}

func TestWriteFrameRejectsOversized(t *testing.T) {
	payload := make([]byte, 100)
	var buf bytes.Buffer
	if err := WriteFrame(&buf, payload, 50); err == nil {
		t.Fatal("WriteFrame accepted oversized payload")
	}
}

func TestWriteFrameRejectsEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, nil, 1024); err == nil {
		t.Fatal("WriteFrame accepted empty payload")
	}
}

func TestReadFrameRejectsOversized(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00, 0x01, 0x00}) // length = 256
	buf.Write(make([]byte, 256))
	if _, err := ReadFrame(&buf, 100); err == nil {
		t.Fatal("ReadFrame accepted oversized frame")
	}
}

func TestZeroBytes(t *testing.T) {
	value := []byte{0x01, 0x02, 0x03, 0x04}
	ZeroBytes(value)
	for i, b := range value {
		if b != 0 {
			t.Errorf("ZeroBytes left byte at index %d = 0x%02x", i, b)
		}
	}
}

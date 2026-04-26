package shared

import (
	"testing"
)

func TestDetectTextEncoding_UTF16LE(t *testing.T) {
	header := []byte{0xFF, 0xFE, 'h', 0, 'i', 0}
	enc, err := DetectTextEncoding(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != UTF16LE {
		t.Errorf("expected UTF16LE, got %d", enc)
	}
}

func TestDetectTextEncoding_UTF16BE(t *testing.T) {
	header := []byte{0xFE, 0xFF, 0, 'h', 0, 'i'}
	enc, err := DetectTextEncoding(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != UTF16BE {
		t.Errorf("expected UTF16BE, got %d", enc)
	}
}

func TestDetectTextEncoding_Binary(t *testing.T) {
	header := []byte{'h', 'e', 'l', 0, 'l', 'o'}
	_, err := DetectTextEncoding(header)
	if err == nil {
		t.Fatal("expected error for binary data")
	}
}

func TestDetectTextEncoding_UTF8(t *testing.T) {
	header := []byte("hello world")
	enc, err := DetectTextEncoding(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != UTF8 {
		t.Errorf("expected UTF8, got %d", enc)
	}
}

func TestDecodeUTF16_LE(t *testing.T) {
	// BOM + "Hi\n"
	data := []byte{0xFF, 0xFE, 'H', 0, 'i', 0, '\n', 0}
	got, err := DecodeUTF16(data, UTF16LE)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hi\n" {
		t.Errorf("expected %q, got %q", "Hi\n", got)
	}
}

func TestDecodeUTF16_BE(t *testing.T) {
	// BOM + "Hi\n"
	data := []byte{0xFE, 0xFF, 0, 'H', 0, 'i', 0, '\n'}
	got, err := DecodeUTF16(data, UTF16BE)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hi\n" {
		t.Errorf("expected %q, got %q", "Hi\n", got)
	}
}

func TestDecodeUTF16_OddBytes(t *testing.T) {
	// BOM + 3 bytes (odd after BOM)
	data := []byte{0xFF, 0xFE, 'H', 0, 'i'}
	_, err := DecodeUTF16(data, UTF16LE)
	if err == nil {
		t.Fatal("expected error for odd byte count")
	}
}

func TestDecodeUTF16_NonUTF16Encoding(t *testing.T) {
	_, err := DecodeUTF16([]byte{0xFF, 0xFE}, UTF8)
	if err == nil {
		t.Fatal("expected error for non-UTF-16 encoding")
	}
}

func TestDecodeUTF16_TooShort(t *testing.T) {
	_, err := DecodeUTF16([]byte{0xFF}, UTF16LE)
	if err == nil {
		t.Fatal("expected error for data too short")
	}
}

func TestDecodeUTF16_EmptyAfterBOM(t *testing.T) {
	data := []byte{0xFF, 0xFE}
	got, err := DecodeUTF16(data, UTF16LE)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestDecodeUTF16_CRLF(t *testing.T) {
	// BOM + "A\r\nB" in LE
	data := []byte{0xFF, 0xFE, 'A', 0, '\r', 0, '\n', 0, 'B', 0}
	got, err := DecodeUTF16(data, UTF16LE)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "A\r\nB" {
		t.Errorf("expected %q, got %q", "A\r\nB", got)
	}
}

package ingestion

import (
	"testing"

	"github.com/agxp/docpulse/internal/domain"
)

func TestDetectFormat_PDF(t *testing.T) {
	data := []byte("%PDF-1.4 rest of file")
	if got := DetectFormat(data); got != domain.FormatPDFNative {
		t.Errorf("expected pdf_native, got %s", got)
	}
}

func TestDetectFormat_DOCX(t *testing.T) {
	// PK zip magic bytes
	data := []byte{0x50, 0x4B, 0x03, 0x04, 0x00}
	if got := DetectFormat(data); got != domain.FormatDOCX {
		t.Errorf("expected docx, got %s", got)
	}
}

func TestDetectFormat_JPEG(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	if got := DetectFormat(data); got != domain.FormatImage {
		t.Errorf("expected image, got %s", got)
	}
}

func TestDetectFormat_PNG(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}
	if got := DetectFormat(data); got != domain.FormatImage {
		t.Errorf("expected image, got %s", got)
	}
}

func TestDetectFormat_TIFF_LittleEndian(t *testing.T) {
	data := []byte{0x49, 0x49, 0x2A, 0x00}
	if got := DetectFormat(data); got != domain.FormatImage {
		t.Errorf("expected image, got %s", got)
	}
}

func TestDetectFormat_TIFF_BigEndian(t *testing.T) {
	data := []byte{0x4D, 0x4D, 0x00, 0x2A}
	if got := DetectFormat(data); got != domain.FormatImage {
		t.Errorf("expected image, got %s", got)
	}
}

func TestDetectFormat_Unknown(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03}
	if got := DetectFormat(data); got != domain.FormatUnknown {
		t.Errorf("expected unknown, got %s", got)
	}
}

func TestDetectFormat_TooShort(t *testing.T) {
	cases := [][]byte{
		{},
		{0x25},
		{0x25, 0x50},
		{0x25, 0x50, 0x44},
	}
	for _, data := range cases {
		if got := DetectFormat(data); got != domain.FormatUnknown {
			t.Errorf("len=%d: expected unknown, got %s", len(data), got)
		}
	}
}

func TestDetectFormat_PlainText(t *testing.T) {
	data := []byte("Hello, world! This is plain text.")
	if got := DetectFormat(data); got != domain.FormatUnknown {
		t.Errorf("expected unknown for plain text, got %s", got)
	}
}

func TestDetectFormat_PDFMagicOnlyFirstBytes(t *testing.T) {
	// Garbage after the magic bytes is fine — we only check the header
	data := append([]byte("%PDF"), make([]byte, 100)...)
	if got := DetectFormat(data); got != domain.FormatPDFNative {
		t.Errorf("expected pdf_native, got %s", got)
	}
}

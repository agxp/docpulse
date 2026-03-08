package ingestion

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/arman/docint/internal/domain"
)

// TextExtractor routes documents to the appropriate extraction tool.
// System dependencies: poppler-utils (pdftotext), tesseract-ocr, pandoc.
type TextExtractor struct{}

func NewTextExtractor() *TextExtractor {
	return &TextExtractor{}
}

func (e *TextExtractor) Extract(ctx context.Context, data []byte, format domain.DocumentFormat) (string, error) {
	switch format {
	case domain.FormatPDFNative, domain.FormatPDFScanned:
		return extractPDF(ctx, data, format)
	case domain.FormatDOCX:
		return extractDOCX(ctx, data)
	case domain.FormatImage:
		return extractImage(ctx, data)
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

func extractPDF(ctx context.Context, data []byte, format domain.DocumentFormat) (string, error) {
	cmd := exec.CommandContext(ctx, "pdftotext", "-", "-")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	text := strings.TrimSpace(string(out))

	// If pdftotext yields nothing, the PDF is likely scanned — fall back to OCR.
	if text == "" && format == domain.FormatPDFNative {
		return extractImage(ctx, data)
	}
	return text, nil
}

func extractDOCX(ctx context.Context, data []byte) (string, error) {
	cmd := exec.CommandContext(ctx, "pandoc", "--from=docx", "--to=plain", "-")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pandoc: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func extractImage(ctx context.Context, data []byte) (string, error) {
	cmd := exec.CommandContext(ctx, "tesseract", "stdin", "stdout", "--dpi", "300")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tesseract: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

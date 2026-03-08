package ingestion

import "github.com/arman/docint/internal/domain"

// DetectFormat identifies the document format from magic bytes.
// Never trusts file extension — always reads the raw bytes.
func DetectFormat(data []byte) domain.DocumentFormat {
	if len(data) < 4 {
		return domain.FormatUnknown
	}

	// PDF: %PDF
	if data[0] == 0x25 && data[1] == 0x50 && data[2] == 0x44 && data[3] == 0x46 {
		return domain.FormatPDFNative // downgraded to scanned if pdftotext yields nothing
	}

	// DOCX / ZIP family (DOCX is a zip)
	if data[0] == 0x50 && data[1] == 0x4B {
		return domain.FormatDOCX
	}

	// JPEG
	if data[0] == 0xFF && data[1] == 0xD8 {
		return domain.FormatImage
	}

	// PNG
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return domain.FormatImage
	}

	// TIFF (little-endian and big-endian)
	if (data[0] == 0x49 && data[1] == 0x49) || (data[0] == 0x4D && data[1] == 0x4D) {
		return domain.FormatImage
	}

	return domain.FormatUnknown
}

package files

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	"nlserver/internal/repository"
)

func TestExtractAITextPlainText(t *testing.T) {
	parsed, err := extractAIText(repository.File{
		OriginalFileName: "notes.md",
		MimeType:         "text/markdown",
	}, []byte("\ufeff# Title\r\nbody"))
	if err != nil {
		t.Fatalf("extractAIText() error = %v", err)
	}
	if parsed.Parser != "text" {
		t.Fatalf("parser = %q, want text", parsed.Parser)
	}
	if parsed.Text != "# Title\nbody" {
		t.Fatalf("text = %q, want normalized markdown", parsed.Text)
	}
}

func TestExtractAITextDOCX(t *testing.T) {
	data := makeZip(t, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
				<w:body>
					<w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>DOCX</w:t></w:r></w:p>
					<w:p><w:r><w:t>Second line</w:t></w:r></w:p>
				</w:body>
			</w:document>`,
	})
	parsed, err := extractAIText(repository.File{
		OriginalFileName: "sample.docx",
		MimeType:         "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}, data)
	if err != nil {
		t.Fatalf("extractAIText() error = %v", err)
	}
	if parsed.Parser != "docx" {
		t.Fatalf("parser = %q, want docx", parsed.Parser)
	}
	if !strings.Contains(parsed.Text, "Hello\tDOCX") || !strings.Contains(parsed.Text, "Second line") {
		t.Fatalf("text = %q, want docx text", parsed.Text)
	}
}

func TestExtractAITextXLSX(t *testing.T) {
	data := makeZip(t, map[string]string{
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
				<si><t>Name</t></si><si><t>Amount</t></si><si><t>Alice</t></si>
			</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
				<sheetData>
					<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row>
					<row><c t="s"><v>2</v></c><c><v>42</v></c></row>
				</sheetData>
			</worksheet>`,
	})
	parsed, err := extractAIText(repository.File{
		OriginalFileName: "sample.xlsx",
		MimeType:         "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	}, data)
	if err != nil {
		t.Fatalf("extractAIText() error = %v", err)
	}
	if parsed.Parser != "xlsx" {
		t.Fatalf("parser = %q, want xlsx", parsed.Parser)
	}
	if parsed.Text != "Name\tAmount\nAlice\t42" {
		t.Fatalf("text = %q, want worksheet rows", parsed.Text)
	}
}

func TestExtractAITextPDF(t *testing.T) {
	data := []byte("%PDF-1.4\nBT\n(Hello \\(PDF\\)) Tj\n<FEFF4E16754C> Tj\nET\n%%EOF")
	parsed, err := extractAIText(repository.File{
		OriginalFileName: "sample.pdf",
		MimeType:         "application/pdf",
	}, data)
	if err != nil {
		t.Fatalf("extractAIText() error = %v", err)
	}
	if parsed.Parser != "pdf" {
		t.Fatalf("parser = %q, want pdf", parsed.Parser)
	}
	if !strings.Contains(parsed.Text, "Hello (PDF)") || !strings.Contains(parsed.Text, "世界") {
		t.Fatalf("text = %q, want extracted PDF strings", parsed.Text)
	}
}

func TestExtractAITextDOCXIncludesSupplementalParts(t *testing.T) {
	data := makeZip(t, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
				<w:body><w:p><w:r><w:t>Main body</w:t></w:r></w:p></w:body>
			</w:document>`,
		"word/header1.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
				<w:p><w:r><w:t>Header text</w:t></w:r></w:p>
			</w:hdr>`,
		"word/footnotes.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
				<w:footnote><w:p><w:r><w:t>Footnote text</w:t></w:r></w:p></w:footnote>
			</w:footnotes>`,
		"word/comments.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<w:comments xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
				<w:comment><w:p><w:r><w:t>Reviewer comment</w:t></w:r></w:p></w:comment>
			</w:comments>`,
	})
	parsed, err := extractAIText(repository.File{
		OriginalFileName: "complex.docx",
		MimeType:         "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}, data)
	if err != nil {
		t.Fatalf("extractAIText() error = %v", err)
	}
	for _, want := range []string{"Main body", "Header text", "Footnote text", "Reviewer comment"} {
		if !strings.Contains(parsed.Text, want) {
			t.Fatalf("text = %q, want %q", parsed.Text, want)
		}
	}
}

func TestExtractAITextPPTX(t *testing.T) {
	data := makeZip(t, map[string]string{
		"ppt/slides/slide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
				xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
				<p:cSld><p:spTree><p:sp><p:txBody>
					<a:p><a:r><a:t>Quarterly review</a:t></a:r></a:p>
					<a:p><a:r><a:t>Revenue</a:t></a:r><a:r><a:tab/></a:r><a:r><a:t>42</a:t></a:r></a:p>
				</p:txBody></p:sp></p:spTree></p:cSld>
			</p:sld>`,
		"ppt/notesSlides/notesSlide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
			<p:notes xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
				xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
				<p:cSld><p:spTree><p:sp><p:txBody>
					<a:p><a:r><a:t>Speaker note</a:t></a:r></a:p>
				</p:txBody></p:sp></p:spTree></p:cSld>
			</p:notes>`,
	})
	parsed, err := extractAIText(repository.File{
		OriginalFileName: "deck.pptx",
		MimeType:         "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}, data)
	if err != nil {
		t.Fatalf("extractAIText() error = %v", err)
	}
	if parsed.Parser != "pptx" {
		t.Fatalf("parser = %q, want pptx", parsed.Parser)
	}
	for _, want := range []string{"Quarterly review", "Revenue\t42", "Speaker note"} {
		if !strings.Contains(parsed.Text, want) {
			t.Fatalf("text = %q, want %q", parsed.Text, want)
		}
	}
}

func TestValidateCompletedMultipartPartsReportsMissingParts(t *testing.T) {
	partSize := int64(8)
	totalParts := 3
	expectedSize := int64(20)
	_, err := validateCompletedMultipartParts(repository.FileUploadSession{
		PartSizeBytes:     &partSize,
		TotalParts:        &totalParts,
		ExpectedSizeBytes: &expectedSize,
	}, []CompletedPart{
		{PartNumber: 1, ETag: `"etag-1"`, SizeBytes: 8},
		{PartNumber: 3, ETag: "etag-3", SizeBytes: 4},
	})
	if !errors.Is(err, ErrUploadPartMissing) {
		t.Fatalf("error = %v, want ErrUploadPartMissing", err)
	}
	var partErr *UploadPartError
	if !errors.As(err, &partErr) {
		t.Fatalf("error = %T, want UploadPartError", err)
	}
	if len(partErr.MissingPartNumbers) != 1 || partErr.MissingPartNumbers[0] != 2 {
		t.Fatalf("missing parts = %v, want [2]", partErr.MissingPartNumbers)
	}
}

func TestValidateCompletedMultipartPartsNormalizesAndSortsParts(t *testing.T) {
	partSize := int64(8)
	totalParts := 3
	expectedSize := int64(20)
	parts, err := validateCompletedMultipartParts(repository.FileUploadSession{
		PartSizeBytes:     &partSize,
		TotalParts:        &totalParts,
		ExpectedSizeBytes: &expectedSize,
	}, []CompletedPart{
		{PartNumber: 3, ETag: `"etag-3"`, SizeBytes: 4},
		{PartNumber: 1, ETag: "etag-1", SizeBytes: 8},
		{PartNumber: 2, ETag: "etag-2", SizeBytes: 8},
	})
	if err != nil {
		t.Fatalf("validateCompletedMultipartParts() error = %v", err)
	}
	for index, part := range parts {
		wantPartNumber := index + 1
		if part.PartNumber != wantPartNumber {
			t.Fatalf("parts[%d].PartNumber = %d, want %d", index, part.PartNumber, wantPartNumber)
		}
		if strings.Contains(part.ETag, `"`) {
			t.Fatalf("parts[%d].ETag = %q, want normalized ETag", index, part.ETag)
		}
	}
}

func TestValidateCompletedMultipartPartsReportsConflictingDuplicate(t *testing.T) {
	totalParts := 1
	_, err := validateCompletedMultipartParts(repository.FileUploadSession{
		TotalParts: &totalParts,
	}, []CompletedPart{
		{PartNumber: 1, ETag: "etag-1"},
		{PartNumber: 1, ETag: "etag-2"},
	})
	if !errors.Is(err, ErrUploadPartETag) {
		t.Fatalf("error = %v, want ErrUploadPartETag", err)
	}
	var partErr *UploadPartError
	if !errors.As(err, &partErr) {
		t.Fatalf("error = %T, want UploadPartError", err)
	}
	if len(partErr.PartNumbers) != 1 || partErr.PartNumbers[0] != 1 {
		t.Fatalf("part numbers = %v, want [1]", partErr.PartNumbers)
	}
}

func TestNormalizeSHA256Hash(t *testing.T) {
	hash := strings.Repeat("a", 64)
	normalized, ok := normalizeSHA256Hash("SHA256:" + hash)
	if !ok {
		t.Fatal("normalizeSHA256Hash() ok = false, want true")
	}
	if normalized != "sha256:"+hash {
		t.Fatalf("normalized = %q, want sha256:%s", normalized, hash)
	}
	if _, ok := normalizeSHA256Hash("not-a-hash"); ok {
		t.Fatal("normalizeSHA256Hash() ok = true, want false")
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		fileWriter, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
		if _, err := fileWriter.Write([]byte(content)); err != nil {
			t.Fatalf("Write(%q) error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return buffer.Bytes()
}

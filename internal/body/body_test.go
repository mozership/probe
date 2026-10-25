package body

import (
	"os"
	"testing"
)

func TestIsText(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{"empty content type", "", true},
		{"text/plain", "text/plain", true},
		{"text/html", "text/html", true},
		{"application/json", "application/json", true},
		{"application/xml", "application/xml", true},
		{"application/javascript", "application/javascript", true},
		{"text/plain with charset", "text/plain; charset=utf-8", true},
		{"image/png", "image/png", false},
		{"image/jpeg", "image/jpeg", false},
		{"video/mp4", "video/mp4", false},
		{"application/pdf", "application/pdf", false},
		{"application/octet-stream", "application/octet-stream", false},
		{"problem+json", "application/problem+json", true},
		{"vnd.api+json", "application/vnd.api+json", true},
		{"hal+json", "application/hal+json", true},
		{"soap+xml", "application/soap+xml", true},
		{"svg+xml", "image/svg+xml", true},
		{"problem+json with charset", "application/problem+json; charset=utf-8", true},
		{"uppercase", "Application/Problem+JSON", true},
		{"uppercase text", "TEXT/HTML; Charset=UTF-8", true},
		{"malformed parameter", "application/problem+json; charset", true},
		{"garbage", "garbage", false},
		{"garbage with slashes", "///", false},
		{"suffix only in a parameter", "image/png; profile=a+json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isText(tt.contentType)
			if result != tt.expected {
				t.Errorf("isText(%q) = %v, expected %v", tt.contentType, result, tt.expected)
			}
		})
	}
}

func TestIsJSON(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"Application/JSON", true},
		{"application/problem+json", true},
		{"application/vnd.api+json", true},
		{"application/hal+json", true},
		{"application/problem+json; charset", true},
		{"application/soap+xml", false},
		{"image/svg+xml", false},
		{"text/plain; format=application/json", false},
		{"image/png", false},
		{"application/octet-stream", false},
		{"", false},
		{"garbage", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			if got := IsJSON(tt.contentType); got != tt.expected {
				t.Errorf("IsJSON(%q) = %v, expected %v", tt.contentType, got, tt.expected)
			}
		})
	}
}

func TestSaveTemp(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		contentType string
		wantErr     bool
		wantEmpty   bool
	}{
		{
			name:        "empty data",
			data:        []byte{},
			contentType: "image/png",
			wantEmpty:   true,
		},
		{
			name:        "PNG image data",
			data:        []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, // PNG header
			contentType: "image/png",
		},
		{
			name:        "PDF data",
			data:        []byte("%PDF-1.4\n"),
			contentType: "application/pdf",
		},
		{
			name:        "unknown content type",
			data:        []byte("some binary data"),
			contentType: "unknown/type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath, err := saveTemp(tt.data, tt.contentType)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantEmpty {
				if filePath != "" {
					t.Errorf("expected empty filepath, got %q", filePath)
				}
				return
			}

			// Verify file was created
			if filePath == "" {
				t.Error("expected non-empty filepath")
				return
			}

			// Check file exists
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				t.Errorf("file was not created at %q", filePath)
				return
			}

			// Clean up
			defer func() { _ = os.Remove(filePath) }()

			// Verify file contents
			savedData, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("failed to read saved file: %v", err)
			}

			if len(savedData) != len(tt.data) {
				t.Errorf("saved data length = %d, expected %d", len(savedData), len(tt.data))
			}

			for i, b := range tt.data {
				if i >= len(savedData) || savedData[i] != b {
					t.Errorf("saved data differs at byte %d", i)
					break
				}
			}
		})
	}
}

func TestTextOrFile(t *testing.T) {
	tests := []struct {
		name         string
		data         []byte
		contentType  string
		expectedBody string
		expectedFile bool
		wantErr      bool
	}{
		{
			name:         "empty data",
			data:         []byte{},
			contentType:  "text/plain",
			expectedBody: "",
			expectedFile: false,
		},
		{
			name:         "JSON data",
			data:         []byte(`{"message": "hello"}`),
			contentType:  "application/json",
			expectedBody: `{"message": "hello"}`,
			expectedFile: false,
		},
		{
			name:         "HTML data",
			data:         []byte("<html><body>Hello</body></html>"),
			contentType:  "text/html",
			expectedBody: "<html><body>Hello</body></html>",
			expectedFile: false,
		},
		{
			name:         "PNG image data",
			data:         []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			contentType:  "image/png",
			expectedBody: "",
			expectedFile: true,
		},
		{
			name:         "PDF data",
			data:         []byte("%PDF-1.4\n"),
			contentType:  "application/pdf",
			expectedBody: "",
			expectedFile: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyString, filePath, err := TextOrFile(tt.data, tt.contentType)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if bodyString != tt.expectedBody {
				t.Errorf("body = %q, expected %q", bodyString, tt.expectedBody)
			}

			if tt.expectedFile {
				if filePath == "" {
					t.Error("expected non-empty filepath for binary data")
				} else {
					// Clean up
					defer func() { _ = os.Remove(filePath) }()

					// Verify file exists
					if _, err := os.Stat(filePath); os.IsNotExist(err) {
						t.Errorf("binary file was not created at %q", filePath)
					}
				}
			} else {
				if filePath != "" {
					t.Errorf("expected empty filepath for text data, got %q", filePath)
				}
			}
		})
	}
}

func TestExtension(t *testing.T) {
	tests := []struct {
		contentType string
		expected    string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"video/mp4", ".mp4"},
		{"application/pdf", ".pdf"},
		{"text/plain", ".txt"},
		{"application/json", ".json"},
		{"unknown/type", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := extension(tt.contentType)
			if result != tt.expected {
				t.Errorf("extension(%q) = %q, expected %q", tt.contentType, result, tt.expected)
			}
		})
	}
}

func TestSaveTemp_OwnerOnly(t *testing.T) {
	path, err := saveTemp([]byte("private screenshot"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600: the temporary directory is shared", info.Mode().Perm())
	}
}

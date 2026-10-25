// Package binary turns raw response payloads into either printable text or a
// file on disk, based on the declared MIME type.
package binary

import (
	"crypto/rand"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// mediaType returns the media type of a Content-Type value, lowercased and
// without parameters such as charset. A value mime.ParseMediaType refuses,
// such as one with a malformed parameter, is taken up to its first
// semicolon, so a type that names text is still read as text.
func mediaType(contentType string) string {
	if mt, _, err := mime.ParseMediaType(contentType); err == nil {
		return mt
	}
	mt, _, _ := strings.Cut(contentType, ";")
	return strings.TrimSpace(strings.ToLower(mt))
}

// IsJSONMediaType reports whether a Content-Type value names JSON:
// application/json, or a structured syntax type such as
// application/problem+json, ignoring parameters such as charset.
func IsJSONMediaType(contentType string) bool {
	mt := mediaType(contentType)
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

// isTextualMimeType determines if the given MIME type represents textual data
func isTextualMimeType(contentType string) bool {
	if contentType == "" {
		return true // Default to text if no content type
	}

	mt := mediaType(contentType)

	// A structured syntax suffix names the syntax the body is written in,
	// so application/problem+json is JSON and image/svg+xml is XML.
	if IsJSONMediaType(contentType) || strings.HasSuffix(mt, "+xml") {
		return true
	}

	// Textual MIME types
	textualTypes := []string{
		"text/",
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-javascript",
		"application/x-www-form-urlencoded",
	}

	for _, textType := range textualTypes {
		if strings.HasPrefix(mt, textType) {
			return true
		}
	}

	return false
}

// SaveBinaryToTempFile saves binary data to a temporary file and returns the file path
func SaveBinaryToTempFile(data []byte, contentType string) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	// Generate a unique filename
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random filename: %w", err)
	}

	filename := fmt.Sprintf("probe_binary_%x", randBytes)

	// Determine file extension from MIME type
	if ext := getExtensionFromMimeType(contentType); ext != "" {
		filename += ext
	}

	// Create file in temporary directory
	tempDir := os.TempDir()
	filePath := filepath.Join(tempDir, filename)

	// Write data to file. The temporary directory is shared with other
	// users and the data can be private, such as a screenshot of a signed-in
	// page, so only the owner may read it; O_EXCL refuses a file or symlink
	// already sitting at the name instead of writing through it.
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Write(data); err != nil {
		_ = os.Remove(filePath) // Clean up on error
		return "", fmt.Errorf("failed to write data to temp file: %w", err)
	}

	return filePath, nil
}

// getExtensionFromMimeType returns the appropriate file extension for a MIME type
func getExtensionFromMimeType(contentType string) string {
	if contentType == "" {
		return ""
	}

	mimeType := strings.Split(contentType, ";")[0]
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))

	// Preferred extensions for common types (override mime package defaults)
	extensionMap := map[string]string{
		"image/jpeg":       ".jpg",
		"image/jpg":        ".jpg",
		"image/png":        ".png",
		"image/gif":        ".gif",
		"image/webp":       ".webp",
		"image/svg+xml":    ".svg",
		"video/mp4":        ".mp4",
		"video/webm":       ".webm",
		"video/quicktime":  ".mov",
		"audio/mp3":        ".mp3",
		"audio/mpeg":       ".mp3",
		"audio/wav":        ".wav",
		"application/pdf":  ".pdf",
		"application/zip":  ".zip",
		"application/gzip": ".gz",
		"text/plain":       ".txt",
		"application/json": ".json",
	}

	// Check our preferred extensions first
	if ext, exists := extensionMap[mimeType]; exists {
		return ext
	}

	// Fallback to mime package for other types
	if exts, err := mime.ExtensionsByType(mimeType); err == nil && len(exts) > 0 {
		return exts[0] // Return the first/most common extension
	}

	return ""
}

// ProcessHttpBody processes HTTP body data based on Content-Type
// Returns (bodyString, filePath, error)
func ProcessHttpBody(data []byte, contentType string) (string, string, error) {
	if len(data) == 0 {
		return "", "", nil
	}

	if isTextualMimeType(contentType) {
		// Return as text body
		return string(data), "", nil
	} else {
		// Save as binary file
		filePath, err := SaveBinaryToTempFile(data, contentType)
		if err != nil {
			return "", "", err
		}
		return "", filePath, nil
	}
}

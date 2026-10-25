// Package body reads a response body by its Content-Type: a body of a
// textual type is kept as text, and any other body, such as an image or a
// PDF, is written to a temporary file.
package body

import (
	"crypto/rand"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// TextOrFile returns the body as text when contentType names a textual
// type, or else writes it to a temporary file that only the owner may read
// and returns the file's path. An empty body returns neither.
func TextOrFile(data []byte, contentType string) (text, path string, err error) {
	if len(data) == 0 {
		return "", "", nil
	}
	if isText(contentType) {
		return string(data), "", nil
	}
	path, err = saveTemp(data, contentType)
	if err != nil {
		return "", "", err
	}
	return "", path, nil
}

// IsJSON reports whether a Content-Type value names JSON:
// application/json, or a structured syntax type such as
// application/problem+json, ignoring parameters such as charset.
func IsJSON(contentType string) bool {
	mt := mediaType(contentType)
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

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

// textualTypes are the prefixes of the media types read as text.
var textualTypes = []string{
	"text/",
	"application/json",
	"application/xml",
	"application/javascript",
	"application/x-javascript",
	"application/x-www-form-urlencoded",
}

// isText reports whether a Content-Type value names a type read as text.
// A body without a Content-Type is read as text.
func isText(contentType string) bool {
	if contentType == "" {
		return true
	}

	// A structured syntax suffix names the syntax the body is written in,
	// so application/problem+json is JSON and image/svg+xml is XML.
	mt := mediaType(contentType)
	if IsJSON(contentType) || strings.HasSuffix(mt, "+xml") {
		return true
	}

	for _, prefix := range textualTypes {
		if strings.HasPrefix(mt, prefix) {
			return true
		}
	}
	return false
}

// saveTemp writes data to a new file in the temporary directory, named
// with the extension of contentType, and returns the file's path. Empty
// data writes no file and returns an empty path.
func saveTemp(data []byte, contentType string) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random filename: %w", err)
	}
	filename := fmt.Sprintf("probe_binary_%x", randBytes) + extension(contentType)
	filePath := filepath.Join(os.TempDir(), filename)

	// The temporary directory is shared with other users and the data can
	// be private, such as a screenshot of a signed-in page, so only the
	// owner may read it; O_EXCL refuses a file or symlink already sitting
	// at the name instead of writing through it.
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Write(data); err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("failed to write data to temp file: %w", err)
	}
	return filePath, nil
}

// extensions are the extensions preferred for common types over what the
// mime package gives.
var extensions = map[string]string{
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

// extension returns the file extension for a Content-Type value, or an
// empty string for a type it does not know.
func extension(contentType string) string {
	if contentType == "" {
		return ""
	}
	mt := mediaType(contentType)
	if ext, ok := extensions[mt]; ok {
		return ext
	}
	if exts, err := mime.ExtensionsByType(mt); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

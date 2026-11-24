package http

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	formContentType      = "application/x-www-form-urlencoded"
	defaultFileMediaType = "application/octet-stream"
)

// takeFormBody turns form or multipart, when one of them is given, into the
// body of the request and the Content-Type it is sent with, and removes it
// from m. The form body is text and is carried in body, where it is shown as
// sent. The multipart body may hold the bytes of files, so it is returned as
// a payload for the request alone, and spec is what was written, to be shown
// in its place.
func takeFormBody(m map[string]any) (payload []byte, spec any, contentType string, err error) {
	form, hasForm := m["form"]
	mp, hasMultipart := m["multipart"]
	switch {
	case hasForm && hasMultipart:
		return nil, nil, "", errors.New("form and multipart cannot be given together")
	case !hasForm && !hasMultipart:
		return nil, nil, "", nil
	}
	if _, ok := m["body"]; ok {
		key := "form"
		if hasMultipart {
			key = "multipart"
		}
		return nil, nil, "", fmt.Errorf("%s and body cannot be given together", key)
	}

	if hasForm {
		delete(m, "form")
		body, err := encodeForm(form)
		if err != nil {
			return nil, nil, "", err
		}
		m["body"] = body
		return nil, nil, formContentType, nil
	}

	delete(m, "multipart")
	payload, contentType, err = encodeMultipart(mp)
	if err != nil {
		return nil, nil, "", err
	}
	return payload, mp, contentType, nil
}

// encodeForm encodes form, a map of field names and values, as
// application/x-www-form-urlencoded. A list sends the field once for each of
// its values. Fields are sorted by name.
func encodeForm(form any) (string, error) {
	fields, ok := form.(map[string]any)
	if !ok {
		return "", errors.New("form must be a map of field names and values")
	}
	values := url.Values{}
	for name, v := range fields {
		for _, e := range fieldValues(v) {
			s, err := formValue(e)
			if err != nil {
				return "", fmt.Errorf("form.%s %w", name, err)
			}
			values.Add(name, s)
		}
	}
	return values.Encode(), nil
}

// multipartPart is one part of a multipart/form-data body.
type multipartPart struct {
	name        string
	filename    string
	contentType string
	data        []byte
	file        bool
}

// encodeMultipart encodes spec, a map of field names and values, as
// multipart/form-data. A value that is a map is a file, read from file or
// given as content; any other is a text field, and a list sends the field
// once for each of its values. Text fields come first and files after them,
// each sorted by name, since some servers, such as S3 for a POST upload, take
// only the fields that come before the file. The values of a list keep their
// order among the text fields or among the files, so a list that holds both
// sends its text values first.
func encodeMultipart(spec any) ([]byte, string, error) {
	fields, ok := spec.(map[string]any)
	if !ok {
		return nil, "", errors.New("multipart must be a map of field names and values")
	}

	var texts, files []multipartPart
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		for _, v := range fieldValues(fields[name]) {
			if f, ok := v.(map[string]any); ok {
				p, err := filePart(name, f)
				if err != nil {
					return nil, "", err
				}
				files = append(files, p)
				continue
			}
			s, err := formValue(v)
			if err != nil {
				return nil, "", fmt.Errorf("multipart.%s %w", name, err)
			}
			texts = append(texts, multipartPart{name: name, data: []byte(s)})
		}
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range append(texts, files...) {
		if err := writePart(w, p); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// filePart reads the file part of the field name from f. Its filename is the
// base name of file, or the field name for content, and its media type is
// worked out from the extension of the filename.
func filePart(name string, f map[string]any) (multipartPart, error) {
	for k := range f {
		switch k {
		case "file", "content", "filename", "content_type":
		default:
			return multipartPart{}, fmt.Errorf("multipart.%s takes file, content, filename and content_type, not %s", name, k)
		}
	}

	file, hasFile := f["file"]
	content, hasContent := f["content"]
	p := multipartPart{name: name, file: true}
	switch {
	case hasFile && hasContent:
		return multipartPart{}, fmt.Errorf("multipart.%s takes file or content, not both", name)
	case hasFile:
		path, ok := file.(string)
		if !ok || path == "" {
			return multipartPart{}, fmt.Errorf("multipart.%s.file must be a path", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return multipartPart{}, fmt.Errorf("multipart.%s: %w", name, err)
		}
		p.data = data
		p.filename = filepath.Base(path)
	case hasContent:
		s, err := formValue(content)
		if err != nil {
			return multipartPart{}, fmt.Errorf("multipart.%s.content %w", name, err)
		}
		p.data = []byte(s)
		p.filename = name
	default:
		return multipartPart{}, fmt.Errorf("multipart.%s needs file or content", name)
	}

	if v, ok := f["filename"]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return multipartPart{}, fmt.Errorf("multipart.%s.filename must be a string", name)
		}
		p.filename = s
	}

	p.contentType = mime.TypeByExtension(filepath.Ext(p.filename))
	if p.contentType == "" {
		p.contentType = defaultFileMediaType
	}
	if v, ok := f["content_type"]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return multipartPart{}, fmt.Errorf("multipart.%s.content_type must be a string", name)
		}
		// A line break would end the header and begin another.
		if strings.ContainsAny(s, "\r\n") {
			return multipartPart{}, fmt.Errorf("multipart.%s.content_type must not contain a line break", name)
		}
		p.contentType = s
	}

	return p, nil
}

// writePart writes p to w. A file part carries its filename and media type,
// which multipart.Writer.CreateFormFile would fix to application/octet-stream.
// The names are escaped as WriteField escapes them, line breaks included.
func writePart(w *multipart.Writer, p multipartPart) error {
	if !p.file {
		return w.WriteField(p.name, string(p.data))
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", multipart.FileContentDisposition(p.name, p.filename))
	h.Set("Content-Type", p.contentType)
	pw, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = pw.Write(p.data)
	return err
}

// fieldValues returns the values a field is sent with: each value of a list,
// or the value itself.
func fieldValues(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return []any{v}
}

// formValue returns a value of a field as it is sent: a string as it is, a
// number or a boolean as written, and a missing one as empty.
func formValue(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case int, int64, uint64, bool:
		return fmt.Sprint(v), nil
	default:
		return "", errors.New("must be a string, a number or a boolean")
	}
}

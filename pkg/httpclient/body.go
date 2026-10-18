package httpclient

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// File is a multipart file part. Content is streamed once, never read by Build.
// The built request body closes Content when it implements io.Closer. Supply a
// fresh reader for another request. Filename is metadata, never a path to open.
type File struct {
	Filename    string
	ContentType string
	Content     io.Reader
}

func validateContentType(format, declared string) error {
	mediaType, params, err := mime.ParseMediaType(declared)
	if err != nil {
		return errors.New("httpclient: invalid Content-Type")
	}
	if format == "raw" {
		return nil
	}
	if charset := params["charset"]; charset != "" && !strings.EqualFold(charset, "utf-8") {
		return errors.New("httpclient: encoded bodies use UTF-8")
	}
	valid := false
	switch format {
	case "json":
		valid = mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
	case "xml":
		valid = mediaType == "application/xml" || mediaType == "text/xml" || strings.HasSuffix(mediaType, "+xml")
	case "form":
		valid = mediaType == "application/x-www-form-urlencoded"
	case "multipart":
		valid = mediaType == "multipart/form-data" && len(params) == 0
	}
	if !valid {
		return errors.New("httpclient: Content-Type conflicts with body encoding; multipart boundaries are generated")
	}
	return nil
}

func encodeBody(value reflect.Value, format string) (io.Reader, string, error) {
	input := value.Interface()
	switch format {
	case "json":
		body, err := json.Marshal(input)
		return bytes.NewReader(body), "application/json", err
	case "xml":
		body, err := xml.Marshal(input)
		return bytes.NewReader(body), "application/xml", err
	case "raw":
		if isNil(value) {
			return nil, "", nil
		}
		switch body := input.(type) {
		case string:
			return strings.NewReader(body), "application/octet-stream", nil
		case []byte:
			return bytes.NewReader(body), "application/octet-stream", nil
		case io.Reader:
			return body, "application/octet-stream", nil
		default:
			return nil, "", errors.New("raw body must be a string, []byte or io.Reader")
		}
	case "form", "multipart":
		parts, err := formParts(input, format == "multipart")
		if err != nil {
			return nil, "", err
		}
		if format == "form" {
			values := make(url.Values)
			for _, part := range parts {
				values.Add(part.name, part.value)
			}
			return strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil
		}
		boundary := multipart.NewWriter(io.Discard).Boundary()
		return &multipartBody{parts: parts, boundary: boundary}, "multipart/form-data; boundary=" + boundary, nil
	default:
		return nil, "", errors.New("unsupported body encoding")
	}
}

type formPart struct {
	name, value string
	file        *File
}

func formParts(input any, files bool) ([]formPart, error) {
	var parts []formPart
	if values, ok := input.(url.Values); ok {
		for _, key := range slices.Sorted(maps.Keys(values)) {
			for _, value := range values[key] {
				parts = append(parts, formPart{name: key, value: value})
			}
		}
		return parts, validateParts(parts)
	}
	value, err := structValue(input)
	if err != nil {
		return nil, errors.New("form body must be a struct or url.Values")
	}
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		tag, ok := field.Tag.Lookup("form")
		if !ok || tag == "-" {
			continue
		}
		name, option, hasOption := strings.Cut(tag, ",")
		if !field.IsExported() || name == "" || hasOption && option != "omitempty" {
			return nil, fmt.Errorf("invalid form tag on %s", field.Name)
		}
		item := value.Field(i)
		if hasOption && emptyValue(item) {
			continue
		}
		if files {
			if file, ok := item.Interface().(File); ok {
				parts = append(parts, formPart{name: name, file: &file})
				continue
			}
			if file, ok := item.Interface().(*File); ok {
				if file != nil {
					copy := *file
					parts = append(parts, formPart{name: name, file: &copy})
				}
				continue
			}
			if list, ok := item.Interface().([]File); ok {
				for _, file := range list {
					copy := file
					parts = append(parts, formPart{name: name, file: &copy})
				}
				continue
			}
		}
		values, err := parameterValues(item, false)
		if err != nil {
			return nil, fmt.Errorf("form field %s: %w", field.Name, err)
		}
		for _, item := range values {
			parts = append(parts, formPart{name: name, value: item})
		}
	}
	return parts, validateParts(parts)
}

func validateParts(parts []formPart) error {
	for _, part := range parts {
		if part.name == "" || !validHeaderValue(part.name) {
			return errors.New("invalid form field name")
		}
		if part.file == nil {
			continue
		}
		file := part.file
		if file.Filename == "" || !validHeaderValue(file.Filename) || file.Content == nil || isNil(reflect.ValueOf(file.Content)) {
			return errors.New("multipart file needs a filename and non-nil reader")
		}
		if file.ContentType != "" {
			if _, _, err := mime.ParseMediaType(file.ContentType); err != nil || !validHeaderValue(file.ContentType) {
				return errors.New("invalid file content type")
			}
		}
	}
	return nil
}

func isNil(value reflect.Value) bool {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}

// multipartBody starts its producer only on the first read. Closing an unsent
// request never leaves a blocked producer. Closing during a send releases the
// pipe and any closable source readers; other readers must unblock themselves.
type multipartBody struct {
	parts        []formPart
	boundary     string
	mu           sync.Mutex
	reader       *io.PipeReader
	closed       bool
	closeSources sync.Once
}

func (b *multipartBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if b.reader == nil {
		reader, writer := io.Pipe()
		b.reader = reader
		go func() {
			err := b.write(writer)
			b.releaseSources()
			_ = writer.CloseWithError(err)
		}()
	}
	reader := b.reader
	b.mu.Unlock()
	return reader.Read(p)
}

func (b *multipartBody) Close() error {
	b.mu.Lock()
	b.closed = true
	if b.reader != nil {
		_ = b.reader.Close()
	}
	b.mu.Unlock()
	b.releaseSources()
	return nil
}

func (b *multipartBody) releaseSources() {
	b.closeSources.Do(func() {
		for _, part := range b.parts {
			if part.file != nil {
				closeBody(part.file.Content)
			}
		}
	})
}

func (b *multipartBody) write(destination io.Writer) error {
	writer := multipart.NewWriter(destination)
	if err := writer.SetBoundary(b.boundary); err != nil {
		return err
	}
	for _, part := range b.parts {
		if part.file == nil {
			if err := writer.WriteField(part.name, part.value); err != nil {
				return err
			}
			continue
		}
		file := part.file
		contentType := file.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header := textproto.MIMEHeader{
			"Content-Disposition": {mime.FormatMediaType("form-data", map[string]string{"name": part.name, "filename": file.Filename})},
			"Content-Type":        {contentType},
		}
		out, err := writer.CreatePart(header)
		if err != nil {
			return err
		}
		if _, err = io.Copy(out, file.Content); err != nil {
			return err
		}
	}
	return writer.Close()
}

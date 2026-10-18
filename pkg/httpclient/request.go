package httpclient

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// Request is an empty marker embedded in an operation struct with method and
// path tags. Parameter fields use path, query or header tags. A body field uses
// body:"json", "xml", "form", "multipart" or "raw". No registration is needed.
//
//	type GetProductRequest struct {
//	    httpclient.Request `method:"GET" path:"products/{id}"`
//	    ID string `path:"id"`
//	}
type Request struct{}

type requestField struct {
	index      int
	kind, name string
	omitEmpty  bool
}
type requestPlan struct {
	method, path string
	fields       []requestField
	err          error
}

var plans sync.Map // reflect.Type -> *requestPlan; definitions are immutable.
var requestType = reflect.TypeFor[Request]()

func buildRequest(ctx context.Context, snapshot *clientState, input any) (*http.Request, error) {
	if ctx == nil {
		return nil, errors.New("httpclient: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if snapshot.url == nil {
		return nil, errors.New("httpclient: BaseURL is required for tagged requests")
	}
	value, err := structValue(input)
	if err != nil {
		return nil, err
	}
	plan := planFor(value.Type())
	if plan.err != nil {
		return nil, plan.err
	}
	path := plan.path
	query := make(url.Values)
	header := snapshot.cfg.Header.Clone()
	var bodyField *requestField
	for _, field := range plan.fields {
		if field.kind == "body" {
			copy := field
			bodyField = &copy
			continue
		}
		values, err := parameterValues(value.Field(field.index), field.omitEmpty)
		if err != nil {
			return nil, fmt.Errorf("httpclient: field %s: %w", value.Type().Field(field.index).Name, err)
		}
		switch field.kind {
		case "path":
			if len(values) != 1 || values[0] == "" {
				return nil, fmt.Errorf("httpclient: path parameter %s needs one nonempty value", field.name)
			}
			for _, segment := range strings.Split(values[0], "/") {
				if segment == "." || segment == ".." {
					return nil, fmt.Errorf("httpclient: path parameter %s contains a dot segment", field.name)
				}
			}
			path = strings.ReplaceAll(path, "{"+field.name+"}", url.PathEscape(values[0]))
		case "query":
			for _, item := range values {
				query.Add(field.name, item)
			}
		case "header":
			if len(values) > 0 {
				header[http.CanonicalHeaderKey(field.name)] = values
			}
		}
	}
	if strings.ContainsAny(path, "{}") {
		return nil, errors.New("httpclient: unresolved path parameter")
	}
	// The explicit relative prefix keeps a colon in a leading path value from
	// being interpreted as a URI scheme. The template itself was validated above.
	relative, err := url.Parse("./" + strings.TrimPrefix(path, "/"))
	if err != nil || relative.IsAbs() || relative.Host != "" || relative.Opaque != "" {
		return nil, errors.New("httpclient: path must be relative to BaseURL")
	}
	relative.RawQuery = query.Encode()
	endpoint := snapshot.url.ResolveReference(relative)
	header, err = cleanHeader(header)
	if err != nil {
		return nil, err
	}
	// Validate the URL and method before creating any body or acquiring streams.
	request, err := http.NewRequestWithContext(ctx, plan.method, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header = header
	if bodyField == nil {
		return request, nil
	}
	if declared := header.Get("Content-Type"); declared != "" {
		if err := validateContentType(bodyField.name, declared); err != nil {
			return nil, err
		}
	}
	body, contentType, err := encodeBody(value.Field(bodyField.index), bodyField.name)
	if err != nil {
		return nil, fmt.Errorf("httpclient: body: %w", err)
	}
	if contentType != "" {
		if header.Get("Content-Type") == "" || bodyField.name == "multipart" {
			header.Set("Content-Type", contentType)
		}
	}
	request, err = http.NewRequestWithContext(ctx, plan.method, endpoint.String(), body)
	if err != nil {
		closeBody(body)
		return nil, err
	}
	request.Header = header
	return request, nil
}

func structValue(input any) (reflect.Value, error) {
	value := reflect.ValueOf(input)
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			break
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return reflect.Value{}, errors.New("httpclient: request must be a non-nil struct or pointer to struct")
	}
	return value, nil
}

func planFor(t reflect.Type) *requestPlan {
	if cached, ok := plans.Load(t); ok {
		return cached.(*requestPlan)
	}
	plan := &requestPlan{}
	plan.err = plan.inspect(t)
	actual, _ := plans.LoadOrStore(t, plan)
	return actual.(*requestPlan)
}

func (plan *requestPlan) inspect(t reflect.Type) error {
	marker := false
	seen := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type == requestType && field.Anonymous {
			if marker {
				return errors.New("httpclient: embed only one Request marker")
			}
			marker = true
			plan.method = field.Tag.Get("method")
			var ok bool
			plan.path, ok = field.Tag.Lookup("path")
			if !validToken(plan.method) || !ok {
				return errors.New("httpclient: Request requires method and path tags")
			}
			continue
		}
		if _, ok := field.Tag.Lookup("method"); ok {
			return errors.New("httpclient: method belongs on the embedded Request")
		}
		count := 0
		for _, kind := range []string{"path", "query", "header", "body"} {
			tag, ok := field.Tag.Lookup(kind)
			if !ok {
				continue
			}
			count++
			if count > 1 || !field.IsExported() {
				return fmt.Errorf("httpclient: field %s has conflicting tags or is unexported", field.Name)
			}
			name, option, hasOption := strings.Cut(tag, ",")
			if name == "" || name == "-" || hasOption && (option != "omitempty" || kind == "path" || kind == "body") {
				return fmt.Errorf("httpclient: invalid %s tag on %s", kind, field.Name)
			}
			key := kind + ":" + name
			if kind == "body" {
				key = "body"
				switch name {
				case "json", "xml", "form", "multipart", "raw":
				default:
					return fmt.Errorf("httpclient: unsupported body encoding %s", name)
				}
			}
			if kind == "header" {
				key = kind + ":" + strings.ToLower(name)
				if _, err := cleanHeader(http.Header{name: {""}}); err != nil {
					return err
				}
			}
			if kind == "path" && !validToken(name) {
				return fmt.Errorf("httpclient: invalid path parameter name on %s", field.Name)
			}
			if seen[key] {
				return fmt.Errorf("httpclient: duplicate %s field", kind)
			}
			seen[key] = true
			plan.fields = append(plan.fields, requestField{index: i, kind: kind, name: name, omitEmpty: hasOption})
		}
		if count == 0 && (field.Name == "Body" || field.Tag.Get("json") != "" || field.Tag.Get("form") != "" || field.Tag.Get("xml") != "") {
			return fmt.Errorf("httpclient: field %s needs an explicit body encoding; payload tags belong inside Body", field.Name)
		}
	}
	if !marker {
		return errors.New("httpclient: embed httpclient.Request with method and path tags")
	}
	if strings.HasPrefix(plan.path, "//") || strings.ContainsAny(plan.path, "?#") {
		return errors.New("httpclient: path must not contain an authority, query or fragment")
	}
	remaining := plan.path
	for _, field := range plan.fields {
		if field.kind != "path" {
			continue
		}
		placeholder := "{" + field.name + "}"
		if !strings.Contains(remaining, placeholder) {
			return fmt.Errorf("httpclient: path does not contain parameter %s", field.name)
		}
		remaining = strings.ReplaceAll(remaining, placeholder, "value")
	}
	if strings.ContainsAny(remaining, "{}") {
		return errors.New("httpclient: missing or malformed path parameter declaration")
	}
	parsed, err := url.Parse(remaining)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Opaque != "" {
		return errors.New("httpclient: invalid relative path")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return errors.New("httpclient: path must not contain dot segments")
		}
	}
	return nil
}

func parameterValues(value reflect.Value, omitEmpty bool) ([]string, error) {
	return encodeParameter(value, omitEmpty, true)
}

func encodeParameter(value reflect.Value, omitEmpty, allowList bool) ([]string, error) {
	if omitEmpty && emptyValue(value) {
		return nil, nil
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil, nil
		}
		if marshaler, ok := value.Interface().(encoding.TextMarshaler); ok {
			text, err := marshaler.MarshalText()
			return []string{string(text)}, err
		}
		value = value.Elem()
	}
	if marshaler, ok := value.Interface().(encoding.TextMarshaler); ok {
		text, err := marshaler.MarshalText()
		return []string{string(text)}, err
	}
	if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		if !allowList {
			return nil, fmt.Errorf("nested parameter collections are unsupported: %s", value.Type())
		}
		var values []string
		for i := 0; i < value.Len(); i++ {
			items, err := encodeParameter(value.Index(i), false, false)
			if err != nil {
				return nil, err
			}
			values = append(values, items...)
		}
		return values, nil
	}
	var text string
	switch value.Kind() {
	case reflect.String:
		text = value.String()
	case reflect.Bool:
		text = strconv.FormatBool(value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		text = strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		text = strconv.FormatUint(value.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		text = strconv.FormatFloat(value.Float(), 'g', -1, value.Type().Bits())
	default:
		return nil, fmt.Errorf("unsupported parameter type %s", value.Type())
	}
	return []string{text}, nil
}

func emptyValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String:
		return value.Len() == 0
	default:
		return value.IsZero()
	}
}

package httpclient_test

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/httpclient"
)

func TestBodyEncodings(t *testing.T) {
	type payload struct {
		Name    string   `json:"name" xml:"name"`
		XMLName xml.Name `json:"-" xml:"contact"`
	}
	type jsonRequest struct {
		httpclient.Request `method:"POST" path:"contacts"`
		Body               payload `body:"json"`
	}
	type xmlRequest struct {
		httpclient.Request `method:"POST" path:"contacts"`
		Body               payload `body:"xml"`
	}
	type formBody struct {
		Email string   `form:"email"`
		Tags  []string `form:"tag"`
		Empty string   `form:"empty,omitempty"`
	}
	type formRequest struct {
		httpclient.Request `method:"POST" path:"contacts"`
		Body               formBody `body:"form"`
	}
	type valuesRequest struct {
		httpclient.Request `method:"POST" path:"contacts"`
		Body               url.Values `body:"form"`
	}
	type rawRequest struct {
		httpclient.Request `method:"PUT" path:"contacts"`
		Body               any    `body:"raw"`
		ContentType        string `header:"Content-Type,omitempty"`
	}
	cases := []struct {
		name              string
		request           any
		contentType, body string
	}{
		{"json", jsonRequest{Body: payload{Name: "Alex"}}, "application/json", `{"name":"Alex"}`},
		{"xml", xmlRequest{Body: payload{Name: "Alex"}}, "application/xml", "<contact><name>Alex</name></contact>"},
		{"form", formRequest{Body: formBody{Email: "a+b@example.com", Tags: []string{"one", "two"}}}, "application/x-www-form-urlencoded", "email=a%2Bb%40example.com&tag=one&tag=two"},
		{"values", valuesRequest{Body: url.Values{"q": {"a b", "c"}}}, "application/x-www-form-urlencoded", "q=a+b&q=c"},
		{"text", rawRequest{Body: "text", ContentType: "text/plain"}, "text/plain", "text"},
		{"binary", rawRequest{Body: []byte{0, 1, 2}}, "application/octet-stream", string([]byte{0, 1, 2})},
		{"reader", rawRequest{Body: strings.NewReader("stream")}, "application/octet-stream", "stream"},
		{"nil reader", rawRequest{Body: (*strings.Reader)(nil)}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != tc.body || r.Header.Get("Content-Type") != tc.contentType {
					t.Errorf("body=%q type=%s err=%v", body, r.Header.Get("Content-Type"), err)
				}
				w.WriteHeader(204)
			}))
			defer server.Close()
			client := newClient(t, httpclient.Config{BaseURL: server.URL})
			response, err := client.Do(context.Background(), tc.request)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 204 {
				t.Fatal(response.StatusCode)
			}
		})
	}
}

func TestInvalidDefinitionsFailBeforeSending(t *testing.T) {
	type noMarker struct{ ID string }
	type noMethod struct {
		httpclient.Request `path:"items"`
	}
	type noPath struct {
		httpclient.Request `method:"GET"`
	}
	type missingPath struct {
		httpclient.Request `method:"GET" path:"items/{id}"`
	}
	type extraPath struct {
		httpclient.Request `method:"GET" path:"items"`
		ID                 string `path:"id"`
	}
	type absolute struct {
		httpclient.Request `method:"GET" path:"https://other.example/items"`
	}
	type authority struct {
		httpclient.Request `method:"GET" path:"//other.example/items"`
	}
	type traversal struct {
		httpclient.Request `method:"GET" path:"%2e%2e/items"`
	}
	type conflict struct {
		httpclient.Request `method:"GET" path:"items"`
		Value              string `query:"x" header:"X"`
	}
	type bodies struct {
		httpclient.Request `method:"POST" path:"items"`
		A                  string `body:"raw"`
		B                  string `body:"json"`
	}
	type unknownEncoding struct {
		httpclient.Request `method:"POST" path:"items"`
		Body               string `body:"yaml"`
	}
	type malformedOption struct {
		httpclient.Request `method:"GET" path:"items"`
		Value              string `query:"x,optional"`
	}
	type complexParameter struct {
		httpclient.Request `method:"GET" path:"items"`
		Value              map[string]string `query:"x"`
	}
	type untaggedBody struct {
		httpclient.Request `method:"POST" path:"items"`
		Body               string
	}
	type invalidHeader struct {
		httpclient.Request `method:"GET" path:"items"`
		Value              string `header:"X-Test"`
	}
	cases := []any{nil, (*getRequest)(nil), "string", noMarker{}, noMethod{}, noPath{}, missingPath{}, extraPath{}, absolute{}, authority{}, traversal{}, conflict{}, bodies{}, unknownEncoding{}, malformedOption{}, complexParameter{}, untaggedBody{}, invalidHeader{Value: "a\r\nInjected: yes"}, getRequest{ID: ""}, getRequest{ID: "../escape"}}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com/v1", Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		t.Error("invalid request reached transport")
		return nil, errors.New("unexpected request")
	})})
	for _, input := range cases {
		t.Run(inputName(input), func(t *testing.T) {
			if _, err := client.Do(context.Background(), input); err == nil {
				t.Fatalf("accepted %T", input)
			}
		})
	}
}

func inputName(value any) string {
	if value == nil {
		return "nil"
	}
	return reflect.TypeOf(value).String()
}

func TestContextAndQueryValues(t *testing.T) {
	type request struct {
		httpclient.Request `method:"GET" path:"/events"`
		At                 time.Time `query:"at"`
		Enabled            bool      `query:"enabled"`
	}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com/v1/"})
	at := time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	built, err := client.Build(ctx, request{At: at})
	if err != nil {
		t.Fatal(err)
	}
	if built.Context() != ctx || built.URL.Path != "/v1/events" || built.URL.Query().Get("at") != at.Format(time.RFC3339) || built.URL.Query().Get("enabled") != "false" {
		t.Fatalf("built=%+v", built)
	}
	cancel()
	if _, err := client.Build(ctx, request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if _, err := client.Build(nil, request{}); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestRawBytesSupportNativeReplay(t *testing.T) {
	type request struct {
		httpclient.Request `method:"POST" path:"items"`
		Body               []byte `body:"raw"`
	}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com"})
	built, err := client.Build(context.Background(), request{Body: []byte("payload")})
	if err != nil {
		t.Fatal(err)
	}
	defer built.Body.Close()
	if built.GetBody == nil || built.ContentLength != 7 {
		t.Fatal("lost native body length/replay")
	}
	body, err := built.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "payload" {
		t.Fatalf("replay=%q %v", data, err)
	}
}

func TestJSONDecodingRejectsTrailingData(t *testing.T) {
	response := &httpclient.Response{Body: []byte(`{"name":"one"}{"name":"two"}`)}
	var value any
	if err := response.DecodeJSON(&value); err == nil {
		t.Fatal("accepted two JSON values")
	}
	response.Body = []byte(`<contact><name>Alex</name></contact>`)
	var contact struct {
		Name string `xml:"name"`
	}
	if err := response.DecodeXML(&contact); err != nil || contact.Name != "Alex" {
		t.Fatalf("XML=%+v %v", contact, err)
	}
}

func TestBodyContentTypeCompatibility(t *testing.T) {
	type request struct {
		httpclient.Request `method:"POST" path:"items"`
		Type               string          `header:"Content-Type"`
		Body               map[string]bool `body:"json"`
	}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com"})
	for _, contentType := range []string{"application/json; charset=utf-8", "application/merge-patch+json"} {
		built, err := client.Build(context.Background(), request{Type: contentType, Body: map[string]bool{"active": false}})
		if err != nil {
			t.Fatal(err)
		}
		_ = built.Body.Close()
		if built.Header.Get("Content-Type") != contentType {
			t.Fatal("lost explicit content type")
		}
	}
	for _, contentType := range []string{"text/plain", "application/json; charset=iso-8859-1"} {
		if _, err := client.Build(context.Background(), request{Type: contentType}); err == nil {
			t.Fatal("accepted conflicting encoding")
		}
	}
}

func TestNestedParameterCollectionsAreRejected(t *testing.T) {
	type request struct {
		httpclient.Request `method:"GET" path:"items"`
		Values             []any `query:"value"`
	}
	values := make([]any, 1)
	values[0] = values
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com"})
	if _, err := client.Build(context.Background(), request{Values: values}); err == nil {
		t.Fatal("accepted recursive parameter list")
	}
}

func TestEscapedPrefixAndLeadingColonValue(t *testing.T) {
	type request struct {
		httpclient.Request `method:"GET" path:"{id}"`
		ID                 string `path:"id"`
	}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com/tenant%2F"})
	built, err := client.Build(context.Background(), request{ID: "urn:product:1"})
	if err != nil {
		t.Fatal(err)
	}
	if built.URL.String() != "https://example.com/tenant%2F/urn:product:1" {
		t.Fatalf("URL=%s", built.URL)
	}
}

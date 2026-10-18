package httpclient_test

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wisalgroup/labena/pkg/httpclient"
)

type uploadRequest struct {
	httpclient.Request `method:"POST" path:"files"`
	Body               struct {
		Title string            `form:"title"`
		Files []httpclient.File `form:"file"`
	} `body:"multipart"`
}

func TestMultipartStreamsFieldsAndFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			return
		}
		var names, values []string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				return
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Error(err)
				return
			}
			names = append(names, part.FormName())
			values = append(values, string(data))
			if part.FormName() == "file" && part.Header.Get("Content-Type") != "text/plain" {
				t.Error("file content type lost")
			}
		}
		if strings.Join(names, ",") != "title,file,file" || strings.Join(values, ",") != "Documents,one,two" {
			t.Errorf("parts=%v %v", names, values)
		}
		w.WriteHeader(201)
	}))
	defer server.Close()
	first, second := &trackedBody{Reader: strings.NewReader("one")}, &trackedBody{Reader: strings.NewReader("two")}
	input := uploadRequest{}
	input.Body.Title = "Documents"
	input.Body.Files = []httpclient.File{{Filename: "a.txt", ContentType: "text/plain", Content: first}, {Filename: "b.txt", ContentType: "text/plain", Content: second}}
	client := newClient(t, httpclient.Config{BaseURL: server.URL})
	response, err := client.Do(context.Background(), input)
	if err != nil || response.StatusCode != 201 {
		t.Fatalf("upload=%v %v", response, err)
	}
	if !first.closed.Load() || !second.closed.Load() {
		t.Fatal("file readers not closed")
	}
}

type unreadSource struct {
	trackedBody
	reads int
}

func (r *unreadSource) Read(p []byte) (int, error) { r.reads++; return r.trackedBody.Read(p) }

func TestMultipartBuildIsLazyAndAbandonmentClosesFiles(t *testing.T) {
	file := &unreadSource{trackedBody: trackedBody{Reader: strings.NewReader("content")}}
	input := uploadRequest{}
	input.Body.Files = []httpclient.File{{Filename: "a.txt", Content: file}}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com"})
	request, err := client.Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if file.reads != 0 {
		t.Fatal("Build read a file")
	}
	if request.GetBody != nil {
		t.Fatal("stream claimed to be replayable")
	}
	_ = request.Body.Close()
	if !file.closed.Load() || file.reads != 0 {
		t.Fatal("abandoned body read or leaked file")
	}
}

type blockedSource struct {
	entered, closed chan struct{}
	once            sync.Once
}

func (b *blockedSource) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockedSource) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestMultipartCloseUnblocksProducer(t *testing.T) {
	source := &blockedSource{entered: make(chan struct{}), closed: make(chan struct{})}
	input := uploadRequest{}
	input.Body.Files = []httpclient.File{{Filename: "blocked.txt", Content: source}}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com"})
	request, err := client.Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, request.Body); done <- err }()
	select {
	case <-source.entered:
	case <-time.After(time.Second):
		t.Fatal("producer did not start")
	}
	_ = request.Body.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed upload succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("upload blocked after close")
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("file unavailable") }

func TestMultipartSourceErrorReachesReader(t *testing.T) {
	input := uploadRequest{}
	input.Body.Files = []httpclient.File{{Filename: "failed.txt", Content: failedReader{}}}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com"})
	request, err := client.Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer request.Body.Close()
	_, err = io.ReadAll(request.Body)
	if err == nil || !strings.Contains(err.Error(), "file unavailable") {
		t.Fatalf("source error=%v", err)
	}
}

func TestMultipartFilenameQuoting(t *testing.T) {
	input := uploadRequest{}
	input.Body.Files = []httpclient.File{{Filename: `a"b.txt`, Content: strings.NewReader("data")}}
	client := newClient(t, httpclient.Config{BaseURL: "https://example.com"})
	request, err := client.Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer request.Body.Close()
	_, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(request.Body, params["boundary"])
	_, err = reader.NextPart()
	if err != nil {
		t.Fatal(err)
	} // title
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if part.FileName() != `a"b.txt` {
		t.Fatalf("filename=%q", part.FileName())
	}
}

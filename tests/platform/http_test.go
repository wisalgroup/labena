package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gocloud.dev/blob"
	"gocloud.dev/blob/fileblob"

	identityhttp "github.com/wisalgroup/labena/internal/modules/identity/delivery/http"
	"github.com/wisalgroup/labena/internal/platform/api"
	"github.com/wisalgroup/labena/internal/platform/auth"
	mediaservice "github.com/wisalgroup/labena/internal/platform/media"
	mediahttp "github.com/wisalgroup/labena/internal/platform/media/integration"
)

type authenticatedOwner struct{ owner uuid.UUID }

func (a authenticatedOwner) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: a.owner, SessionID: uuid.New()}, nil
}
func TestAssetHTTPPostgres(t *testing.T) {
	for _, visibility := range []string{"public", "private"} {
		t.Run(visibility, func(t *testing.T) { checkAssetHTTP(t, visibility) })
	}
}
func checkAssetHTTP(t *testing.T, visibility string) {
	db, owner := databaseFixture(t)
	bucket, err := fileblob.OpenBucket(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bucket.Close()
	svc, err := mediaservice.New(db, map[string]*blob.Bucket{"local": bucket}, "local", mediaservice.Config{BaseURL: "/media", SigningKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	makeRouter := func(owner uuid.UUID) *api.Server {
		r := newAPI(t)

		identityhttp.RegisterRoutes(nil, api.Authenticated(r, authenticatedOwner{owner}), identityhttp.Routes{Media: svc})
		mediahttp.RegisterContentHTTP(r, svc)
		return r
	}
	router := makeRouter(owner)
	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer test")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "private.html")
	if err != nil {
		t.Fatal(err)
	}
	file.Write([]byte("<html><script>example</script></html>"))
	if visibility == "private" {
		writer.WriteField("visibility", visibility)
	}
	writer.Close()
	req := httptest.NewRequest("POST", "/media", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if out.Code != 201 {
		t.Fatal(out.Code, out.Body.String())
	}
	var result struct {
		Data struct {
			ID         uuid.UUID `json:"id"`
			URL        string    `json:"url"`
			Visibility string    `json:"visibility"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil || result.Data.ID == uuid.Nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Body.String(), "assets/") || strings.Contains(out.Body.String(), `"disk"`) {
		t.Fatal("storage identity exposed")
	}
	if result.Data.URL == "" || result.Data.Visibility != visibility {
		t.Fatal("missing asset URL/visibility", out.Body.String())
	}
	contentPath := "/media/" + result.Data.ID.String() + "/content"
	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest("GET", contentPath, nil))
	want := 200
	if visibility == "private" {
		want = 404
	}
	if anonymous.Code != want {
		t.Fatal("anonymous content", anonymous.Code)
	}
	issue := httptest.NewRequest("POST", "/media/"+result.Data.ID.String()+"/url", strings.NewReader(`{"expires_in":60}`))
	issue.Header.Set("Content-Type", "application/json")
	issue.Header.Set("Authorization", "Bearer test")
	issued := httptest.NewRecorder()
	router.ServeHTTP(issued, issue)
	var link struct {
		Data mediaservice.Link `json:"data"`
	}
	if issued.Code != 200 || json.Unmarshal(issued.Body.Bytes(), &link) != nil || link.Data.URL == "" {
		t.Fatal("issue link", issued.Code, issued.Body.String())
	}
	linked := httptest.NewRecorder()
	router.ServeHTTP(linked, httptest.NewRequest("GET", link.Data.URL, nil))
	if linked.Code != 200 || !strings.Contains(linked.Body.String(), "<html>") || !strings.HasPrefix(linked.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("signed content", linked.Code, linked.Header())
	}
	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest("GET", link.Data.URL+"x", nil))
	if invalid.Code != 404 {
		t.Fatal("tampered link", invalid.Code)
	}
	download := call("GET", "/media/"+result.Data.ID.String()+"/download")
	if download.Code != 200 || download.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") || download.Header().Get("X-Content-Type-Options") != "nosniff" || download.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(download.Code, download.Header())
	}
	other := makeRouter(uuid.New())
	req = httptest.NewRequest("DELETE", "/media/"+result.Data.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer test")
	denied := httptest.NewRecorder()
	other.ServeHTTP(denied, req)
	if denied.Code != 404 {
		t.Fatal("cross-owner delete", denied.Code)
	}
	for range 2 {
		deleted := call("DELETE", "/media/"+result.Data.ID.String())
		if deleted.Code != 204 {
			t.Fatal(deleted.Code, deleted.Body.String())
		}
	}
	revoked := httptest.NewRecorder()
	router.ServeHTTP(revoked, httptest.NewRequest("GET", link.Data.URL, nil))
	if revoked.Code != 404 {
		t.Fatal("deleted signed content", revoked.Code)
	}
	if got := call("GET", "/media/"+result.Data.ID.String()+"/download"); got.Code != 404 {
		t.Fatal(got.Code)
	}
}

func newAPI(t testing.TB, options ...api.Options) *api.Server {
	t.Helper()
	var opts api.Options
	if len(options) > 0 {
		opts = options[0]
	}
	server, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

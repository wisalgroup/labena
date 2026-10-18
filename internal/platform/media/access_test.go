package media

import (
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPublicAndPrivateAccess(t *testing.T) {
	for _, visibility := range []Visibility{"", Public, Private} {
		t.Run(string(visibility), func(t *testing.T) {
			library, repo, _ := fixture(t)
			library.options = Config{BaseURL: "https://files.example/api/v1/media", SigningKey: []byte(strings.Repeat("k", 32))}.normalized()
			owner := Owner{Type: "user", ID: uuid.NewString()}
			a, err := library.Upload(t.Context(), UploadInput{Owner: owner, Filename: "test.txt", Body: strings.NewReader("hello"), Visibility: visibility, Policy: Policy{MaxBytes: 100}})
			if err != nil {
				t.Fatal(err)
			}
			want := visibility
			if want == "" {
				want = Public
			}
			if a.Visibility != want {
				t.Fatal(a.Visibility)
			}
			linkURL, err := library.URL(t.Context(), a)
			if err != nil {
				t.Fatal(err)
			}
			_, reader, err := library.OpenContent(t.Context(), a.ID, 0, "")
			if want == Private {
				if !errors.Is(err, ErrAssetNotFound) {
					t.Fatal("anonymous private access", err)
				}
				if !strings.HasSuffix(linkURL.URL, "/download") {
					t.Fatal("private URL")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(reader)
				reader.Close()
				if err != nil || string(body) != "hello" {
					t.Fatal("public content", err)
				}
				if !strings.HasSuffix(linkURL.URL, "/content") {
					t.Fatal("public URL")
				}
			}
			other := Owner{Type: "admin", ID: owner.ID}
			if _, err := library.TemporaryURL(t.Context(), other, a.ID, time.Minute); !errors.Is(err, ErrAssetNotFound) {
				t.Fatal("cross-owner signing", err)
			}
			link, err := library.TemporaryURL(t.Context(), owner, a.ID, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(link.URL)
			expires, _ := strconv.ParseInt(u.Query().Get("expires"), 10, 64)
			signature := u.Query().Get("signature")
			if expires != link.ExpiresAt.Unix() {
				t.Fatal("expiry mismatch")
			}
			_, reader, err = library.OpenContent(t.Context(), a.ID, expires, signature)
			if err != nil {
				t.Fatal("signed access", err)
			}
			reader.Close()
			for _, tc := range []struct {
				id        uuid.UUID
				expires   int64
				signature string
			}{
				{uuid.New(), expires, signature}, {a.ID, expires + 60, signature}, {a.ID, expires, signature + "x"},
				{a.ID, time.Now().Unix() - 1, library.signature(a.ID, time.Now().Unix()-1)},
			} {
				if _, _, err := library.OpenContent(t.Context(), tc.id, tc.expires, tc.signature); !errors.Is(err, ErrAssetNotFound) {
					t.Fatal("invalid link accepted", err)
				}
			}
			repo.asset.Status = AssetPending
			if _, _, err := library.OpenContent(t.Context(), a.ID, expires, signature); !errors.Is(err, ErrAssetNotFound) {
				t.Fatal("pending content served", err)
			}
			repo.asset.Status = AssetReady
			if err := library.Delete(t.Context(), owner, a.ID); err != nil {
				t.Fatal(err)
			}
			if _, _, err := library.OpenContent(t.Context(), a.ID, expires, signature); !errors.Is(err, ErrAssetNotFound) {
				t.Fatal("deleted content served", err)
			}
		})
	}
}

func TestMediaAccessConfigurationAndValidation(t *testing.T) {
	for _, o := range []Config{{BaseURL: "https://user:secret@example.com/media"}, {BaseURL: "//example.com/media"}, {BaseURL: "javascript:bad"}, {BaseURL: "https://example.com/media?key=secret"}, {SigningKey: []byte("short")}} {
		if o.Validate() == nil {
			t.Fatal("invalid options accepted")
		}
	}
	library, repo, _ := fixture(t)
	owner := Owner{Type: "user", ID: uuid.NewString()}
	a, err := library.Upload(t.Context(), UploadInput{Owner: owner, Visibility: Private, Filename: "file.txt", Body: strings.NewReader("hi"), Policy: Policy{MaxBytes: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.TemporaryURL(t.Context(), owner, a.ID, 0); !errors.Is(err, ErrSigningUnavailable) {
		t.Fatal(err)
	}
	for _, ttl := range []time.Duration{-1, time.Nanosecond, 25 * time.Hour} {
		if _, err := library.TemporaryURL(t.Context(), owner, a.ID, ttl); !errors.Is(err, ErrInvalidExpiry) {
			t.Fatal(err)
		}
	}
	calls := repo.calls
	if _, err := library.Upload(t.Context(), UploadInput{Visibility: "invalid"}); !errors.Is(err, ErrInvalidUpload) || repo.calls != calls {
		t.Fatal("invalid visibility wrote metadata", err)
	}
}

func TestDiskVisibilityAndURLMode(t *testing.T) {
	library, _, bucket := fixture(t)
	library.disks["other"] = bucket
	library.options.Disks = map[string]DiskConfig{
		"local": {Visibility: Private},
		"other": {Visibility: Public, URLMode: "direct", PublicURL: "https://cdn.example/uploads/"},
	}
	owner := Owner{Type: "user", ID: uuid.NewString()}
	for _, tc := range []struct {
		disk     string
		override Visibility
		want     Visibility
	}{
		{"", "", Private}, {"", Public, Public}, {"other", "", Public}, {"other", Private, Private},
	} {
		a, err := library.Upload(t.Context(), UploadInput{Owner: owner, Filename: "file.txt", Body: strings.NewReader("hello"), Visibility: tc.override, Policy: Policy{Disk: tc.disk, MaxBytes: 100}})
		if err != nil {
			t.Fatal(err)
		}
		if a.Visibility != tc.want {
			t.Fatalf("visibility = %s, want %s", a.Visibility, tc.want)
		}
		if strings.HasPrefix(a.Key, "public/") != (tc.want == Public) {
			t.Fatal("wrong storage namespace")
		}
		link, err := library.URL(t.Context(), a)
		if tc.disk == "other" && tc.want == Public {
			if err != nil || link.URL != "https://cdn.example/uploads/"+a.Key || !link.ExpiresAt.IsZero() {
				t.Fatal(link, err)
			}
			a.Key = "assets/legacy"
			link, err = library.URL(t.Context(), a)
			if err != nil || !strings.HasSuffix(link.URL, "/content") {
				t.Fatal("legacy object must not assume public storage policy", link, err)
			}
		} else if tc.disk == "other" {
			// Fileblob has no signing configuration: URL returns a safe fallback.
			if err == nil || link == nil || !strings.HasSuffix(link.URL, "/download") {
				t.Fatal("missing signing fallback", link, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiskConfigValidation(t *testing.T) {
	for _, disk := range []DiskConfig{
		{Visibility: "unknown"}, {URLMode: "unknown"}, {URLMode: "direct"},
		{PublicURL: "/uploads"}, {PublicURL: "https://user:password@host/uploads"},
		{PublicURL: "https://host/uploads?signature=secret"}, {PublicURL: "https://host/#fragment"},
	} {
		if disk.Validate() == nil {
			t.Fatal("invalid disk options accepted")
		}
	}
}

package mailview

import (
	"strings"
	"sync"
	"testing"
)

func brand() Brand {
	return Brand{Name: "Example", AccentColor: "#1D4ED8", WebsiteURL: "https://example.com", LogoURL: "https://example.com/logo.png", SupportEmail: "support@example.com"}
}

func TestRender(t *testing.T) {
	r, err := New(brand())
	if err != nil {
		t.Fatal(err)
	}
	content := Email{Subject: "Verify email", Title: "Confirm <email>", Preheader: "A private code awaits", Paragraphs: []string{`<script>alert("x")</script>`}, Code: &Code{Value: "123456", Label: "CODE", Hint: "Expires in 10 minutes"}, Action: &Action{Label: "Continue", URL: "https://example.com/start"}}
	out, err := r.Render(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"123456", "#1D4ED8", "&lt;email&gt;", "&lt;script&gt;", "<!--[if mso]>", `role="presentation"`, `alt="Example logo"`} {
		if !strings.Contains(out.HTML, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(out.HTML, "ZgotmplZ") || strings.Contains(out.HTML, "<script>") {
		t.Fatal("unsafe or filtered HTML")
	}
	for _, want := range []string{"123456", "https://example.com/start", "support@example.com", "Expires in 10 minutes"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("plain text missing %q", want)
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := r.Render(content); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestConfigurationAndEscaping(t *testing.T) {
	for _, change := range []func(*Brand){
		func(b *Brand) { b.AccentColor = "red; background:url(https://evil.test)" },
		func(b *Brand) { b.LogoURL = "javascript:alert(1)" },
		func(b *Brand) { b.WebsiteURL = "https://user:password@example.com" },
		func(b *Brand) { b.SupportEmail = "Name <support@example.com>" },
	} {
		b := brand()
		change(&b)
		if _, err := New(b); err == nil {
			t.Fatal("unsafe brand accepted")
		}
	}
	r, _ := New(brand())
	for _, email := range []Email{
		{Subject: "Bad\r\nBcc: injected", Title: "Title"},
		{Subject: "Subject", Title: "Title", Action: &Action{Label: "Open", URL: "javascript:alert(1)"}},
	} {
		if _, err := r.Render(email); err == nil {
			t.Fatal("unsafe content accepted")
		}
	}
	b := brand()
	b.LogoURL = ""
	r, _ = New(b)
	out, err := r.Render(Email{Subject: "Subject", Title: "Title"})
	if err != nil || strings.Contains(out.HTML, "<img") {
		t.Fatal("text-only branding failed", err)
	}
	if contrastText("#ffffff") != "#000000" || contrastText("#000000") != "#ffffff" {
		t.Fatal("contrast selection failed")
	}
}

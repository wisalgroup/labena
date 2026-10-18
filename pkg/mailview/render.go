// Package mailview renders branded transactional emails independently of delivery.
package mailview

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	texttemplate "text/template"
)

// Brand is copied at construction; it contains public presentation settings only.
type Brand struct {
	Name         string
	AccentColor  string
	LogoURL      string
	WebsiteURL   string
	SupportEmail string
	Footer       string
}

// Email describes semantic content shared by transactional templates. All strings
// are escaped in HTML; callers cannot inject raw HTML, CSS, or JavaScript.
type Email struct {
	Subject    string
	Preheader  string
	Eyebrow    string
	Title      string
	Paragraphs []string
	Code       *Code
	Action     *Action
	Note       string
}

type Code struct {
	Value string
	Label string
	Hint  string
}
type Action struct {
	Label string
	URL   string
}
type Content struct {
	Subject string
	HTML    string
	Text    string
}

// Renderer parses the layout/components once and is safe for concurrent rendering.
// Brand identity is private and copied, so changing config requires reconstruction.
type Renderer struct {
	brand    Brand
	onAccent string
	html     *template.Template
	text     *texttemplate.Template
}

//go:embed templates/*
var files embed.FS

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (b Brand) Validate() error {
	if strings.TrimSpace(b.Name) == "" || len(b.Name) > 100 || !colorPattern.MatchString(b.AccentColor) {
		return errors.New("mailview: brand name and six-digit hex accent color are required")
	}
	if !httpsURL(b.WebsiteURL) || (b.LogoURL != "" && !httpsURL(b.LogoURL)) {
		return errors.New("mailview: website and optional logo must be absolute HTTPS URLs")
	}
	if b.SupportEmail != "" {
		a, err := mail.ParseAddress(b.SupportEmail)
		if err != nil || a.Address != b.SupportEmail {
			return errors.New("mailview: invalid support address")
		}
	}
	return nil
}

func New(brand Brand) (*Renderer, error) {
	if err := brand.Validate(); err != nil {
		return nil, err
	}
	html, err := template.New("email").Funcs(template.FuncMap{
		// Only these constant Outlook wrappers bypass escaping; user content never does.
		"msoStart": func() template.HTML {
			return template.HTML(`<!--[if mso]><table role="presentation" width="600" align="center"><tr><td><![endif]-->`)
		},
		"msoEnd": func() template.HTML { return template.HTML(`<!--[if mso]></td></tr></table><![endif]-->`) },
	}).Option("missingkey=error").ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("mailview: parse HTML: %w", err)
	}
	text, err := texttemplate.New("email.txt").Option("missingkey=error").ParseFS(files, "templates/email.txt")
	if err != nil {
		return nil, fmt.Errorf("mailview: parse text: %w", err)
	}
	return &Renderer{brand: brand, onAccent: contrastText(brand.AccentColor), html: html, text: text}, nil
}

func (r *Renderer) Render(email Email) (Content, error) {
	if strings.TrimSpace(email.Subject) == "" || strings.ContainsAny(email.Subject, "\r\n") || strings.TrimSpace(email.Title) == "" {
		return Content{}, errors.New("mailview: subject and title are required; subject must be a single line")
	}
	if email.Action != nil && (strings.TrimSpace(email.Action.Label) == "" || !httpsURL(email.Action.URL)) {
		return Content{}, errors.New("mailview: action requires a label and an absolute HTTPS URL")
	}
	if email.Code != nil && (email.Code.Value == "" || len(email.Code.Value) > 32 || strings.ContainsAny(email.Code.Value, "\r\n")) {
		return Content{}, errors.New("mailview: invalid code block")
	}
	data := struct {
		Brand    Brand
		OnAccent string
		Email    Email
	}{r.brand, r.onAccent, email}
	var html, text bytes.Buffer
	if err := r.html.ExecuteTemplate(&html, "email", data); err != nil {
		return Content{}, fmt.Errorf("mailview: render HTML: %w", err)
	}
	if err := r.text.ExecuteTemplate(&text, "email.txt", data); err != nil {
		return Content{}, fmt.Errorf("mailview: render text: %w", err)
	}
	return Content{Subject: email.Subject, HTML: html.String(), Text: strings.TrimSpace(text.String()) + "\n"}, nil
}

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

// Select black or white for WCAG AA contrast on any configured accent background.
func contrastText(color string) string {
	var luminance float64
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		value, _ := strconv.ParseUint(color[1+i*2:3+i*2], 16, 8)
		channel := float64(value) / 255
		if channel <= 0.04045 {
			channel /= 12.92
		} else {
			channel = math.Pow((channel+0.055)/1.055, 2.4)
		}
		luminance += channel * weight
	}
	if (luminance+0.05)/0.05 >= 1.05/(luminance+0.05) {
		return "#000000"
	}
	return "#ffffff"
}

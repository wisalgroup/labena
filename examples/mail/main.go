// Render Identity OTP and welcome mail views locally. This command never sends email.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wisalgroup/labena/internal/app/config"
	"github.com/wisalgroup/labena/internal/modules/identity/mailviews"
	"github.com/wisalgroup/labena/pkg/mailview"
)

func main() {
	directory := flag.String("output", "storage/app/mail-preview", "Preview output directory")
	flag.Parse()
	if err := render(*directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func render(directory string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	views, err := mailview.New(cfg.Mail.Brand(cfg.App.AppName))
	if err != nil {
		return err
	}
	email, err := mailviews.SignIn("428691", 10*time.Minute)
	if err != nil {
		return err
	}
	content, err := views.Render(email)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	welcome, err := views.Render(mailviews.NewWelcome("Ada"))
	if err != nil {
		return err
	}
	for name, body := range map[string]string{"otp.html": content.HTML, "otp.txt": content.Text, "welcome.html": welcome.HTML, "welcome.txt": welcome.Text} {
		path := filepath.Join(directory, name)
		if err = os.WriteFile(path, []byte(body), 0o600); err != nil {
			return err
		}
		fmt.Println(path)
	}
	return nil
}

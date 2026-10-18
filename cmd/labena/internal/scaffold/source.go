package scaffold

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const templateModule = "github.com/wisalgroup/labena"

// provenance records the starting source, not a dependency to update with go get.
type provenance struct {
	Repository string `json:"repository"`
	Version    string `json:"version,omitempty"`
	Sum        string `json:"sum,omitempty"`
	Commit     string `json:"commit,omitempty"`
	Local      bool   `json:"local,omitempty"`
	Modified   bool   `json:"modified,omitempty"`
}

type source struct {
	directory string
	files     []string // Local checkouts copy only tracked and non-ignored files.
	origin    provenance
}

func resolveSource(ctx context.Context, options Options) (source, error) {
	origin := provenance{Repository: "https://" + templateModule}
	if options.Source != "" {
		directory, err := filepath.Abs(options.Source)
		if err != nil {
			return source{}, err
		}
		// Require the checkout root: otherwise Git lists paths relative to a subtree.
		prefix, err := run(ctx, directory, "git", "rev-parse", "--show-prefix")
		if err != nil || strings.TrimSpace(string(prefix)) != "" {
			return source{}, errors.New("--source must be the root of a local Labena Git checkout")
		}
		commit, err := run(ctx, directory, "git", "rev-parse", "HEAD")
		if err != nil {
			return source{}, fmt.Errorf("read source commit: %w", err)
		}
		status, err := run(ctx, directory, "git", "status", "--porcelain")
		if err != nil {
			return source{}, fmt.Errorf("read source status: %w", err)
		}
		files, err := run(ctx, directory, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		if err != nil {
			return source{}, fmt.Errorf("list source files: %w", err)
		}
		origin.Local, origin.Modified, origin.Commit = true, len(status) > 0, strings.TrimSpace(string(commit))
		return source{directory: directory, files: strings.Split(strings.TrimSuffix(string(files), "\x00"), "\x00"), origin: origin}, nil
	}
	if !semver.IsValid(options.Version) || strings.Contains(options.Version, "+") {
		return source{}, errors.New("template version must be an exact Go version such as v0.1.1, not a branch or latest")
	}
	// Run outside any application module. The Go command supplies private-module
	// authentication, proxy policy and checksum verification using the user's setup.
	directory, err := os.MkdirTemp("", "labena-download-*")
	if err != nil {
		return source{}, err
	}
	defer os.RemoveAll(directory)
	output, err := run(ctx, directory, "go", "mod", "download", "-json", templateModule+"@"+options.Version)
	if err != nil {
		return source{}, fmt.Errorf("download template: %w; check the published version, Git authentication and GOPRIVATE for private repositories", err)
	}
	var result struct {
		Dir, Version, Sum string
		Origin            struct{ Hash string }
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Dir == "" || result.Sum == "" {
		return source{}, errors.New("download template: incomplete Go module metadata")
	}
	origin.Version, origin.Sum, origin.Commit = result.Version, result.Sum, result.Origin.Hash
	return source{directory: result.Dir, origin: origin}, nil
}

// run invokes fixed tools without a shell. The user's Go/Git credentials and
// proxy settings are intentionally inherited; application/workspace settings are not.
// Captured diagnostics stay private: command errors must not echo credentials.
func run(ctx context.Context, directory, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = directory
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "APP_ENV_FILE=")
	var output, diagnostics limitedBuffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostics
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return output.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4<<20 {
		return 0, errors.New("tool output exceeds 4 MiB")
	}
	return b.Buffer.Write(p)
}

package platform_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wisalgroup/labena/internal/platform/mail"
)

type attachmentReader struct {
	reader               io.Reader
	reads, bytes, closes int
	closeErr             error
	cancel               context.CancelFunc
}

func (r *attachmentReader) Read(p []byte) (int, error) {
	r.reads++
	n, err := r.reader.Read(p)
	r.bytes += n
	if r.cancel != nil {
		r.cancel()
	}
	return n, err
}
func (r *attachmentReader) Close() error { r.closes++; return r.closeErr }

type failedRead struct{ err error }

func (r failedRead) Read([]byte) (int, error) { return 0, r.err }

type failedSeek struct {
	*attachmentReader
	err error
}

func (r failedSeek) Seek(int64, int) (int64, error) { return 0, r.err }

func smtpMessage(readers ...io.Reader) mail.Message {
	m := mail.Message{To: []mail.Address{{Email: "recipient@example.test"}}, Subject: "Report", TextBody: "Attached", CloseAttachments: true}
	for _, reader := range readers {
		m.Attachments = append(m.Attachments, mail.NewAttachment("report.txt", "text/plain", reader))
	}
	return m
}

// A loopback SMTP peer acknowledges DATA, allowing boundary/cleanup tests to
// distinguish accepted sends from failures without contacting a real provider.
func localSMTP(t *testing.T) mail.SMTPConfig {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	certServer := httptest.NewTLSServer(http.NotFoundHandler())
	tlsConfig := certServer.TLS.Clone()
	certServer.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		peer := textproto.NewConn(conn)
		if err := peer.PrintfLine("220 localhost ESMTP"); err != nil {
			return
		}
		secured := false
		for {
			line, err := peer.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO") && !secured:
				if err := peer.PrintfLine("250-localhost\r\n250 STARTTLS"); err != nil {
					return
				}
			case line == "STARTTLS":
				if err := peer.PrintfLine("220 begin TLS"); err != nil {
					return
				}
				secure := tls.Server(conn, tlsConfig)
				if err := secure.Handshake(); err != nil {
					return
				}
				peer = textproto.NewConn(secure)
				secured = true
			case strings.HasPrefix(line, "DATA"):
				if err := peer.PrintfLine("354 send message"); err != nil {
					return
				}
				if _, err := peer.ReadDotBytes(); err != nil {
					return
				}
				if err := peer.PrintfLine("250 accepted"); err != nil {
					return
				}
			case strings.HasPrefix(line, "QUIT"):
				_ = peer.PrintfLine("221 goodbye")
				return
			default:
				if err := peer.PrintfLine("250 OK"); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return mail.SMTPConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, From: mail.Address{Email: "sender@example.test"}, MaxAttachmentBytes: 8, UseTLS: true, InsecureSkipVerify: true}
}

func TestSMTPAttachmentBoundsAndOwnership(t *testing.T) {
	for _, size := range []int{0, 7, 8, 9, 1 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			sender, err := mail.NewSMTP(localSMTP(t))
			if err != nil {
				t.Fatal(err)
			}
			first := &attachmentReader{reader: strings.NewReader("ab")}
			second := &attachmentReader{reader: strings.NewReader(strings.Repeat("x", size))}
			err = sender.Send(t.Context(), smtpMessage(first, second))
			if (size+2 > 8) != errors.Is(err, mail.ErrAttachmentsTooLarge) || (size+2 <= 8 && err != nil) {
				t.Fatal(err)
			}
			if first.bytes+second.bytes > 9 || first.closes != 1 || second.closes != 1 {
				t.Fatalf("reads=%d closes=%d/%d", first.bytes+second.bytes, first.closes, second.closes)
			}
		})
	}
	// The exact aggregate boundary is accepted, including an empty final reader.
	sender, _ := mail.NewSMTP(localSMTP(t))
	if err := sender.Send(t.Context(), smtpMessage(strings.NewReader("12345678"), strings.NewReader(""))); err != nil {
		t.Fatal(err)
	}
}

func TestSMTPAttachmentFailureCleanup(t *testing.T) {
	readFailure, closeFailure := errors.New("read failed"), errors.New("close failed")
	for _, name := range []string{"validation", "canceled", "during-read", "read", "seek", "close", "borrowed", "transport"} {
		t.Run(name, func(t *testing.T) {
			cfg := localSMTP(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first := &attachmentReader{reader: strings.NewReader("abc")}
			last := &attachmentReader{reader: strings.NewReader("def")}
			m := smtpMessage(first, last)
			var want error
			switch name {
			case "validation":
				m.Subject = ""
			case "canceled":
				cancel()
				want = context.Canceled
			case "during-read":
				first.cancel = cancel
				want = context.Canceled
			case "read":
				first.reader = failedRead{readFailure}
				first.closeErr = closeFailure
				want = readFailure
			case "seek":
				m.Attachments[0].Body = failedSeek{first, readFailure}
				want = readFailure
			case "close":
				first.closeErr = closeFailure
				want = closeFailure
			case "borrowed":
				m.CloseAttachments = false
			case "transport":
				cfg.InsecureSkipVerify = false // Reject the test server's untrusted certificate.
			}
			sender, err := mail.NewSMTP(cfg)
			if err != nil {
				t.Fatal(err)
			}
			err = sender.Send(ctx, m)
			if want != nil && !errors.Is(err, want) {
				t.Fatalf("want %v, got %v", want, err)
			}
			if name != "borrowed" && err == nil {
				t.Fatal("missing failure")
			}
			if name == "borrowed" && err != nil {
				t.Fatal(err)
			}
			if name == "read" && !errors.Is(err, closeFailure) {
				t.Fatal("close error lost", err)
			}
			closed := 1
			if name == "borrowed" {
				closed = 0
			}
			if first.closes != closed || last.closes != closed {
				t.Fatal("incomplete cleanup", first.closes, last.closes)
			}
			if (name == "canceled" || name == "validation" || name == "seek") && first.reads != 0 {
				t.Fatal("read before validation/cancellation")
			}
			if name == "during-read" && last.reads != 0 {
				t.Fatal("read continued after cancellation")
			}
		})
	}
}

func BenchmarkSMTPAttachmentOverflow(b *testing.B) {
	sender, err := mail.NewSMTP(mail.SMTPConfig{Host: "127.0.0.1", Port: 1, From: mail.Address{Email: "sender@example.test"}, MaxAttachmentBytes: 1024})
	if err != nil {
		b.Fatal(err)
	}
	data := strings.Repeat("x", 1<<20)
	b.ReportAllocs()
	for b.Loop() {
		reader := &attachmentReader{reader: strings.NewReader(data)}
		if err := sender.Send(b.Context(), smtpMessage(reader)); !errors.Is(err, mail.ErrAttachmentsTooLarge) || reader.bytes != 1025 {
			b.Fatal(err, reader.bytes)
		}
	}
}

package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

var _ Sender = (*SMTP)(nil)

// DefaultMaxAttachmentBytes limits the aggregate raw bytes of immediate attachments.
const DefaultMaxAttachmentBytes int64 = 20 << 20

// ErrAttachmentsTooLarge means SMTP delivery was not attempted because attachment input exceeded its budget.
var ErrAttachmentsTooLarge = errors.New("mail: attachments exceed the configured byte limit")

// SMTPConfig configures the default sender and SMTP connection. UseTLS requires
// STARTTLS; InsecureSkipVerify is only appropriate for controlled test servers.
type SMTPConfig struct {
	Host string
	Port int

	Username string
	Password string

	From Address

	UseTLS bool

	InsecureSkipVerify bool

	// MaxAttachmentBytes bounds all raw attachment bytes in one Send. Zero uses
	// DefaultMaxAttachmentBytes. MIME encoding and message bodies are additional.
	MaxAttachmentBytes int64
}

// Validate checks options without I/O. An omitted default From permits a
// message-level sender override; credentials must be either absent or paired.
func (c SMTPConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" || c.Port < 1 || c.Port > 65535 {
		return errors.New("mail: valid SMTP host and port are required")
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("mail: SMTP username and password must be set together")
	}
	if c.From.Email != "" || c.From.Name != "" {
		address, err := mail.ParseAddress(c.From.Email)
		if err != nil || address.Address != c.From.Email || strings.ContainsAny(c.From.Name, "\r\n") {
			return errors.New("mail: valid default sender email and single-line name are required")
		}
	}
	if c.MaxAttachmentBytes < 0 {
		return errors.New("mail: attachment byte limit must not be negative")
	}
	return nil
}

// SMTP is an immediate Sender. Each Send owns and closes its SMTP connection;
// the configured client is safe for concurrent sends with separate messages.
type SMTP struct {
	client *gomail.Client

	from               Address
	maxAttachmentBytes int64
}

// NewSMTP validates connection options without dialing or starting background work.
func NewSMTP(
	cfg SMTPConfig,
) (*SMTP, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.MaxAttachmentBytes == 0 {
		cfg.MaxAttachmentBytes = DefaultMaxAttachmentBytes
	}

	auth := gomail.SMTPAuthNoAuth
	if cfg.Username != "" {
		auth = gomail.SMTPAuthAutoDiscover
	}
	client, err := gomail.NewClient(
		cfg.Host,
		gomail.WithPort(cfg.Port),
		gomail.WithTimeout(10*time.Second),
		gomail.WithSMTPAuth(auth),
		gomail.WithUsername(cfg.Username),
		gomail.WithPassword(cfg.Password),
	)

	if err != nil {
		return nil, fmt.Errorf(
			"create smtp client: %w",
			err,
		)
	}

	if cfg.UseTLS {
		client.SetTLSPolicy(
			gomail.TLSMandatory,
		)
	}

	if cfg.InsecureSkipVerify {
		client.SetTLSConfig(
			&tls.Config{
				InsecureSkipVerify: true,
			},
		)
	}

	return &SMTP{
		client:             client,
		from:               cfg.From,
		maxAttachmentBytes: cfg.MaxAttachmentBytes,
	}, nil
}

func (m *SMTP) Send(
	ctx context.Context,
	message Message,
) (err error) {
	// Register ownership before validation so every failure path closes readers.
	if message.CloseAttachments {
		defer func() {
			for _, attachment := range message.Attachments {
				if closer, ok := attachment.Body.(io.Closer); ok {
					if closeErr := closer.Close(); closeErr != nil {
						err = errors.Join(err, fmt.Errorf("close mail attachment: %w", closeErr))
					}
				}
			}
		}()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := message.Validate(); err != nil {
		return err
	}

	msg := gomail.NewMsg()

	from := m.from

	if message.From != nil {
		from = *message.From
	}

	if err := msg.FromFormat(
		from.Name,
		from.Email,
	); err != nil {
		return err
	}

	for _, to := range message.To {
		if err := msg.AddToFormat(
			to.Name,
			to.Email,
		); err != nil {
			return err
		}
	}

	for _, cc := range message.CC {
		if err := msg.AddCcFormat(
			cc.Name,
			cc.Email,
		); err != nil {
			return err
		}
	}

	for _, bcc := range message.BCC {
		if err := msg.AddBccFormat(
			bcc.Name,
			bcc.Email,
		); err != nil {
			return err
		}
	}

	if len(message.ReplyTo) > 0 {
		addresses := make([]string, 0, len(message.ReplyTo))

		for _, replyTo := range message.ReplyTo {
			addr := mail.Address{
				Name:    replyTo.Name,
				Address: replyTo.Email,
			}

			addresses = append(
				addresses,
				addr.String(),
			)
		}

		msg.SetGenHeader(
			"Reply-To",
			strings.Join(addresses, ", "),
		)
	}

	msg.Subject(message.Subject)

	switch {
	case message.HTMLBody != "" && message.TextBody != "":
		msg.SetBodyString(
			gomail.TypeTextPlain,
			message.TextBody,
		)

		msg.AddAlternativeString(
			gomail.TypeTextHTML,
			message.HTMLBody,
		)

	case message.HTMLBody != "":
		msg.SetBodyString(
			gomail.TypeTextHTML,
			message.HTMLBody,
		)

	default:
		msg.SetBodyString(
			gomail.TypeTextPlain,
			message.TextBody,
		)
	}

	for key, value := range message.Headers {
		msg.SetGenHeader(
			gomail.Header(key),
			value,
		)
	}

	remaining := m.maxAttachmentBytes
	for _, attachment := range message.Attachments {
		size, err := addAttachment(ctx, msg, attachment, remaining)
		if err != nil {
			return err
		}
		remaining -= size
	}

	return m.client.DialAndSendWithContext(
		ctx,
		msg,
	)
}

// contextReader checks cooperative cancellation between reads. It cannot
// interrupt a reader blocked inside Read; callers must bound that reader's I/O.
type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

func addAttachment(ctx context.Context, msg *gomail.Msg, attachment Attachment, remaining int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if attachment.Filename == "" {
		return 0, errors.New("attachment filename is required")
	}
	if attachment.Body == nil {
		return 0, nil
	}
	if seeker, ok := attachment.Body.(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return 0, fmt.Errorf("seek mail attachment: %w", err)
		}
	}
	reader := contextReader{ctx: ctx, reader: attachment.Body}
	limited := &io.LimitedReader{R: reader, N: remaining}
	data, err := io.ReadAll(limited)
	if err != nil {
		return 0, fmt.Errorf("read mail attachment: %w", err)
	}
	// Probe one extra byte without overflowing remaining or buffering more input.
	if limited.N == 0 {
		n, err := io.CopyN(io.Discard, reader, 1)
		if n != 0 {
			return 0, ErrAttachmentsTooLarge
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("read mail attachment: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	contentType := attachment.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	err = msg.AttachReader(attachment.Filename, bytes.NewReader(data), gomail.WithFileContentType(gomail.ContentType(contentType)))
	return int64(len(data)), err
}

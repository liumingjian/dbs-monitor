package notify

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
)

type Channel interface {
	Send(context.Context, Message) error
}

type TLSMode string

const (
	TLSStartTLS TLSMode = "STARTTLS"
	TLSImplicit TLSMode = "IMPLICIT"
)

type AuthType string

const (
	AuthNone  AuthType = "NONE"
	AuthPlain AuthType = "PLAIN"
	AuthLogin AuthType = "LOGIN"
)

type SMTPConfig struct {
	Host      string
	Port      int
	From      string
	Username  string
	Password  string
	TLSMode   TLSMode
	AuthType  AuthType
	TLSConfig *tls.Config
}

type SMTPChannel struct {
	config SMTPConfig
}

func NewSMTPChannel(config SMTPConfig) *SMTPChannel {
	return &SMTPChannel{config: config}
}

func (channel *SMTPChannel) Send(ctx context.Context, message Message) error {
	address := net.JoinHostPort(channel.config.Host, strconv.Itoa(channel.config.Port))
	dialer := &net.Dialer{}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect SMTP server: %w", err)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			return fmt.Errorf("set SMTP deadline: %w", err)
		}
	}

	tlsConfig := channel.config.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: channel.config.Host}
	} else {
		tlsConfig = tlsConfig.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = channel.config.Host
		}
	}
	if channel.config.TLSMode == TLSImplicit {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("negotiate implicit SMTP TLS: %w", err)
		}
		connection = tlsConnection
	}

	client, err := smtp.NewClient(connection, channel.config.Host)
	if err != nil {
		return fmt.Errorf("start SMTP client: %w", err)
	}
	defer client.Close()
	if channel.config.TLSMode == TLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("negotiate SMTP STARTTLS: %w", err)
		}
	}
	if auth := channel.auth(); auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authenticate SMTP client: %w", err)
		}
	}
	if err := client.Mail(channel.config.From); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP message: %w", err)
	}
	if _, err := writer.Write(encodeMessage(channel.config.From, message)); err != nil {
		writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit SMTP client: %w", err)
	}
	return nil
}

func (channel *SMTPChannel) auth() smtp.Auth {
	switch channel.config.AuthType {
	case AuthPlain:
		return smtp.PlainAuth("", channel.config.Username, channel.config.Password, channel.config.Host)
	case AuthLogin:
		return loginAuth{username: channel.config.Username, password: channel.config.Password}
	default:
		return nil
	}
}

func encodeMessage(from string, message Message) []byte {
	headers := map[string]string{
		"From":         (&mail.Address{Address: from}).String(),
		"To":           (&mail.Address{Address: message.To}).String(),
		"Subject":      mime.QEncoding.Encode("UTF-8", message.Subject),
		"MIME-Version": "1.0",
		"Content-Type": "text/plain; charset=UTF-8",
	}
	var builder strings.Builder
	for _, name := range []string{"From", "To", "Subject", "MIME-Version", "Content-Type"} {
		fmt.Fprintf(&builder, "%s: %s\r\n", name, headers[name])
	}
	builder.WriteString("\r\n")
	scanner := bufio.NewScanner(strings.NewReader(message.Body))
	for scanner.Scan() {
		builder.WriteString(scanner.Text())
		builder.WriteString("\r\n")
	}
	return []byte(builder.String())
}

type loginAuth struct {
	username string
	password string
}

func (auth loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("LOGIN authentication requires TLS")
	}
	return "LOGIN", nil, nil
}

func (auth loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(string(fromServer))
	switch {
	case strings.Contains(prompt, "username"):
		return []byte(auth.username), nil
	case strings.Contains(prompt, "password"):
		return []byte(auth.password), nil
	default:
		return nil, fmt.Errorf("unexpected LOGIN authentication prompt %q", fromServer)
	}
}

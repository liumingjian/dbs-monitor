package notify

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPChannelDeliversOverTLS(t *testing.T) {
	for _, mode := range []TLSMode{TLSStartTLS, TLSImplicit} {
		t.Run(string(mode), func(t *testing.T) {
			address, received, closeServer := startSMTPReceiver(t, mode)
			defer closeServer()
			host, portText, err := net.SplitHostPort(address)
			if err != nil {
				t.Fatal(err)
			}
			port, _ := strconv.Atoi(portText)
			channel := NewSMTPChannel(SMTPConfig{
				Host: host, Port: port, From: "monitor@example.com", TLSMode: mode, AuthType: AuthNone,
				TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}, // Test receiver uses an ephemeral CA.
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := channel.Send(ctx, Message{To: "dba@example.com", Subject: "告警触发", Body: "连接数过高\n"}); err != nil {
				t.Fatalf("send SMTP message: %v", err)
			}
			select {
			case message := <-received:
				for _, required := range []string{"monitor@example.com", "dba@example.com", "=?UTF-8?q?", "连接数过高"} {
					if !strings.Contains(message, required) {
						t.Errorf("received message does not contain %q:\n%s", required, message)
					}
				}
			case <-ctx.Done():
				t.Fatal("SMTP receiver did not receive a message")
			}
		})
	}
}

func startSMTPReceiver(t *testing.T, mode TLSMode) (string, <-chan string, func()) {
	t.Helper()
	tlsConfig := testSMTPServerTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SMTP: %v", err)
	}
	received := make(chan string, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		if mode == TLSImplicit {
			connection = tls.Server(connection, tlsConfig)
		}
		reader := bufio.NewReader(connection)
		writer := bufio.NewWriter(connection)
		writeSMTPLine(writer, "220 localhost ESMTP")
		if !expectSMTPCommand(reader, "EHLO") {
			return
		}
		if mode == TLSStartTLS {
			writeSMTPRaw(writer, "250-localhost\r\n250 STARTTLS\r\n")
			if !expectSMTPCommand(reader, "STARTTLS") {
				return
			}
			writeSMTPLine(writer, "220 Ready to start TLS")
			connection = tls.Server(connection, tlsConfig)
			if err := connection.(*tls.Conn).Handshake(); err != nil {
				return
			}
			reader = bufio.NewReader(connection)
			writer = bufio.NewWriter(connection)
			if !expectSMTPCommand(reader, "EHLO") {
				return
			}
		}
		writeSMTPLine(writer, "250 localhost")
		if !expectSMTPCommand(reader, "MAIL FROM:") {
			return
		}
		writeSMTPLine(writer, "250 OK")
		if !expectSMTPCommand(reader, "RCPT TO:") {
			return
		}
		writeSMTPLine(writer, "250 OK")
		if !expectSMTPCommand(reader, "DATA") {
			return
		}
		writeSMTPLine(writer, "354 End data with <CR><LF>.<CR><LF>")
		var message strings.Builder
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			if line == ".\r\n" {
				break
			}
			message.WriteString(line)
		}
		received <- message.String()
		writeSMTPLine(writer, "250 queued")
		if expectSMTPCommand(reader, "QUIT") {
			writeSMTPLine(writer, "221 bye")
		}
	}()
	return listener.Addr().String(), received, func() { listener.Close() }
}

func expectSMTPCommand(reader *bufio.Reader, prefix string) bool {
	line, err := reader.ReadString('\n')
	return err == nil && strings.HasPrefix(strings.ToUpper(line), prefix)
}

func writeSMTPLine(writer *bufio.Writer, line string) {
	writeSMTPRaw(writer, line+"\r\n")
}

func writeSMTPRaw(writer *bufio.Writer, value string) {
	_, _ = writer.WriteString(value)
	_ = writer.Flush()
}

func testSMTPServerTLS(t *testing.T) *tls.Config {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
	)
	if err != nil {
		t.Fatal(fmt.Errorf("load test SMTP certificate: %w", err))
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
}

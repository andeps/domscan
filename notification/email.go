package notification

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"domscan/availability"
)

type SMTPNotifier struct {
	host                     string
	port                     int
	username, password, from string
	timeout                  time.Duration
}

func NewSMTPNotifier(host string, port int, username, password, from string, timeout time.Duration) *SMTPNotifier {
	return &SMTPNotifier{host: host, port: port, username: username, password: password, from: from, timeout: timeout}
}

func (n *SMTPNotifier) Notify(ctx context.Context, recipient string, results []availability.Result) error {
	recipient, err := validRecipient(recipient)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}
	recipients := []string{recipient}
	address := n.host + ":" + strconv.Itoa(n.port)
	var auth smtp.Auth
	if n.username != "" {
		auth = smtp.PlainAuth("", n.username, n.password, n.host)
	}
	done := make(chan error, 1)
	go func() { done <- n.send(address, auth, recipients, buildMessage(n.from, recipients, results)) }()
	timer := time.NewTimer(n.timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("邮箱发送超时")
	}
}

func (n *SMTPNotifier) send(address string, auth smtp.Auth, recipients []string, message []byte) error {
	if n.port != 465 {
		return smtp.SendMail(address, auth, n.from, recipients, message)
	}
	conn, err := tls.Dial("tcp", address, &tls.Config{ServerName: n.host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, n.host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()
	if auth != nil {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(n.from); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err = client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(message); err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
}

func validRecipient(recipient string) (string, error) {
	recipient = strings.TrimSpace(recipient)
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient || strings.ContainsAny(recipient, "\r\n") {
		return "", fmt.Errorf("登录邮箱地址无效")
	}
	return recipient, nil
}

func buildMessage(from string, to []string, results []availability.Result) []byte {
	var body strings.Builder
	fmt.Fprintf(&body, "发现 %d 个可注册域名：\n\n", len(results))
	for _, result := range results {
		expiration := result.ExpirationTime
		if expiration == "" {
			expiration = "暂无"
		}
		fmt.Fprintf(&body, "- %s（到期时间：%s）\n", result.Domain, expiration)
	}
	subject := mime.QEncoding.Encode("UTF-8", "发现可注册域名")
	return []byte("From: " + from + "\r\nTo: " + strings.Join(to, ", ") + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body.String())
}

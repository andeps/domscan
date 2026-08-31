package notification

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
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
	to                       []string
	timeout                  time.Duration
}

// CheckConnection validates TCP/SMTP connectivity and authentication without sending mail.
func (n *SMTPNotifier) CheckConnection(ctx context.Context) error {
	if n.host == "" || n.port < 1 || n.port > 65535 {
		return fmt.Errorf("SMTP 地址无效")
	}
	dialer := net.Dialer{Timeout: n.timeout}
	var conn net.Conn
	var err error
	if n.port == 465 {
		conn, err = tls.DialWithDialer(&dialer, "tcp", n.host+":"+strconv.Itoa(n.port), &tls.Config{ServerName: n.host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", n.host+":"+strconv.Itoa(n.port))
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败：%w", err)
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, n.host)
	if err != nil {
		return fmt.Errorf("SMTP 握手失败：%w", err)
	}
	defer client.Close()
	if n.username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("SMTP 服务器未声明 AUTH")
		}
		if err := client.Auth(smtp.PlainAuth("", n.username, n.password, n.host)); err != nil {
			return fmt.Errorf("SMTP 认证失败：%w", err)
		}
	}
	return nil
}

func NewSMTPNotifier(host string, port int, username, password, from string, to []string, timeout time.Duration) *SMTPNotifier {
	return &SMTPNotifier{host: host, port: port, username: username, password: password, from: from, to: to, timeout: timeout}
}

func (n *SMTPNotifier) Notify(ctx context.Context, results []availability.Result) error {
	if len(results) == 0 || len(n.to) == 0 {
		return nil
	}
	address := n.host + ":" + strconv.Itoa(n.port)
	var auth smtp.Auth
	if n.username != "" {
		auth = smtp.PlainAuth("", n.username, n.password, n.host)
	}
	done := make(chan error, 1)
	go func() { done <- n.send(address, auth, buildMessage(n.from, n.to, results)) }()
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

func (n *SMTPNotifier) send(address string, auth smtp.Auth, message []byte) error {
	if n.port != 465 {
		return smtp.SendMail(address, auth, n.from, n.to, message)
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
	for _, recipient := range n.to {
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

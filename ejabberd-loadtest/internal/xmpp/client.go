package xmpp

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client is a minimal XMPP client for load testing over plain-text c2s.
type Client struct {
	Host     string
	Port     int
	Domain   string
	User     string
	Password string
	Resource string

	conn    net.Conn
	reader  *bufio.Reader
	dec     *xml.Decoder
	writeMu sync.Mutex

	jid    string
	closed atomic.Bool

	incoming chan IncomingMessage
}

type IncomingMessage struct {
	From    string
	Body    string
	Arrived time.Time
}

type streamFeatures struct {
	XMLName    xml.Name `xml:"http://etherx.jabber.org/streams features"`
	StartTLS   *struct{} `xml:"starttls"`
	Mechanisms struct {
		Mechanism []string `xml:"mechanism"`
	} `xml:"mechanisms"`
	Bind     *struct{} `xml:"bind"`
	Register *struct{} `xml:"register"`
	Session  *struct{} `xml:"session"`
}

const (
	nsClient = "jabber:client"
	nsStream = "http://etherx.jabber.org/streams"
	nsSASL   = "urn:ietf:params:xml:ns:xmpp-sasl"
	nsBind   = "urn:ietf:params:xml:ns:xmpp-bind"
	nsReg    = "jabber:iq:register"
)

// Dial opens a TCP connection and returns a client without authenticating.
func Dial(ctx context.Context, host string, port int, domain string, timeout time.Duration) (*Client, error) {
	d := net.Dialer{Timeout: timeout}
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}
	c := &Client{
		Host:     host,
		Port:     port,
		Domain:   domain,
		conn:     conn,
		reader:   bufio.NewReaderSize(conn, 4096),
		incoming: make(chan IncomingMessage, 32),
	}
	c.dec = xml.NewDecoder(c.reader)
	return c, nil
}

func (c *Client) JID() string { return c.jid }

func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	if c.conn != nil {
		_ = c.writeRaw("</stream:stream>")
		_ = c.conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
		return c.conn.Close()
	}
	return nil
}

func (c *Client) writeRaw(s string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := io.WriteString(c.conn, s)
	return err
}

// openStream sends the initial stream header and returns stream features.
func (c *Client) openStream(readTimeout time.Duration) (*streamFeatures, error) {
	hdr := fmt.Sprintf(`<?xml version='1.0'?><stream:stream to='%s' xmlns='%s' xmlns:stream='%s' version='1.0'>`,
		c.Domain, nsClient, nsStream)
	if err := c.writeRaw(hdr); err != nil {
		return nil, err
	}
	c.dec = xml.NewDecoder(c.reader)
	// Read until we hit stream:features
	deadline := time.Now().Add(readTimeout)
	_ = c.conn.SetReadDeadline(deadline)
	for {
		t, err := c.dec.Token()
		if err != nil {
			return nil, fmt.Errorf("open stream: %w", err)
		}
		if se, ok := t.(xml.StartElement); ok {
			if se.Name.Local == "stream" {
				continue // <stream:stream>
			}
			if se.Name.Local == "features" {
				var f streamFeatures
				if err := c.dec.DecodeElement(&f, &se); err != nil {
					return nil, err
				}
				_ = c.conn.SetReadDeadline(time.Time{})
				return &f, nil
			}
			if se.Name.Local == "error" {
				return nil, fmt.Errorf("stream error from server")
			}
		}
	}
}

// Register performs XEP-0077 in-band registration over a fresh stream.
// Must be called before Authenticate.
func (c *Client) Register(user, pass string, timeout time.Duration) error {
	if _, err := c.openStream(timeout); err != nil {
		return err
	}
	iq := fmt.Sprintf(
		`<iq type='set' id='reg1'><query xmlns='%s'><username>%s</username><password>%s</password></query></iq>`,
		nsReg, xmlEscape(user), xmlEscape(pass))
	if err := c.writeRaw(iq); err != nil {
		return err
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	defer c.conn.SetReadDeadline(time.Time{})
	for {
		t, err := c.dec.Token()
		if err != nil {
			return fmt.Errorf("register read: %w", err)
		}
		if se, ok := t.(xml.StartElement); ok && se.Name.Local == "iq" {
			var iqResp struct {
				Type  string `xml:"type,attr"`
				ID    string `xml:"id,attr"`
				Error struct {
					Code string `xml:"code,attr"`
					Text string `xml:",innerxml"`
				} `xml:"error"`
			}
			if err := c.dec.DecodeElement(&iqResp, &se); err != nil {
				return err
			}
			if iqResp.Type == "result" {
				return nil
			}
			return fmt.Errorf("register failed: %s", iqResp.Error.Text)
		}
	}
}

// Authenticate performs SASL PLAIN and binds a resource.
func (c *Client) Authenticate(user, pass, resource string, timeout time.Duration) error {
	features, err := c.openStream(timeout)
	if err != nil {
		return err
	}
	hasPlain := false
	for _, m := range features.Mechanisms.Mechanism {
		if strings.EqualFold(m, "PLAIN") {
			hasPlain = true
			break
		}
	}
	if !hasPlain {
		return errors.New("server does not offer SASL PLAIN")
	}
	authStr := "\x00" + user + "\x00" + pass
	b64 := base64.StdEncoding.EncodeToString([]byte(authStr))
	msg := fmt.Sprintf(`<auth xmlns='%s' mechanism='PLAIN'>%s</auth>`, nsSASL, b64)
	if err := c.writeRaw(msg); err != nil {
		return err
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	t, err := c.dec.Token()
	if err != nil {
		return fmt.Errorf("auth read: %w", err)
	}
	se, ok := t.(xml.StartElement)
	if !ok {
		return fmt.Errorf("auth: unexpected token")
	}
	switch se.Name.Local {
	case "success":
		c.dec.Skip()
	case "failure":
		c.dec.Skip()
		return errors.New("sasl failure")
	default:
		return fmt.Errorf("auth: unexpected element %s", se.Name.Local)
	}
	// Restart stream after SASL success.
	features, err = c.openStream(timeout)
	if err != nil {
		return err
	}
	if features.Bind == nil {
		return errors.New("server did not offer bind")
	}
	bindIQ := fmt.Sprintf(
		`<iq type='set' id='bind1'><bind xmlns='%s'><resource>%s</resource></bind></iq>`,
		nsBind, xmlEscape(resource))
	if err := c.writeRaw(bindIQ); err != nil {
		return err
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		tok, err := c.dec.Token()
		if err != nil {
			return fmt.Errorf("bind read: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "iq" {
			var iqResp struct {
				Type string `xml:"type,attr"`
				Bind struct {
					JID string `xml:"jid"`
				} `xml:"bind"`
			}
			if err := c.dec.DecodeElement(&iqResp, &se); err != nil {
				return err
			}
			if iqResp.Type != "result" {
				return errors.New("bind failed")
			}
			c.jid = iqResp.Bind.JID
			break
		}
	}
	_ = c.conn.SetReadDeadline(time.Time{})
	return nil
}

// SendPresence sends an available presence.
func (c *Client) SendPresence() error {
	return c.writeRaw(`<presence/>`)
}

// SendMessage sends a chat message.
func (c *Client) SendMessage(to, body, id string) error {
	s := fmt.Sprintf(`<message type='chat' to='%s' id='%s'><body>%s</body></message>`,
		xmlEscape(to), xmlEscape(id), xmlEscape(body))
	return c.writeRaw(s)
}

// Incoming returns the channel of incoming messages.
func (c *Client) Incoming() <-chan IncomingMessage { return c.incoming }

// ReadLoop continuously reads stanzas and dispatches messages.
// Must be called in its own goroutine after Authenticate.
func (c *Client) ReadLoop(ctx context.Context) error {
	defer close(c.incoming)
	for {
		if c.closed.Load() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		tok, err := c.dec.Token()
		if err != nil {
			if c.closed.Load() {
				return nil
			}
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "message":
			var m struct {
				From string `xml:"from,attr"`
				ID   string `xml:"id,attr"`
				Type string `xml:"type,attr"`
				Body string `xml:"body"`
			}
			if err := c.dec.DecodeElement(&m, &se); err != nil {
				return err
			}
			if m.Body != "" {
				select {
				case c.incoming <- IncomingMessage{From: m.From, Body: m.Body, Arrived: time.Now()}:
				default:
					// drop to avoid blocking under overload
				}
			}
		default:
			if err := c.dec.Skip(); err != nil {
				return err
			}
		}
	}
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

package loadtest

import "time"

type Config struct {
	Host   string
	Port   int
	Domain string

	UserPrefix string
	Password   string

	TargetConnections int
	RampPerSec        int // new connections per second
	ConnectTimeout    time.Duration
	AuthTimeout       time.Duration

	// Messaging
	MessageIntervalMs int    // per-user interval between messages
	MessageBody       string // default body; worker may append nonce
	PairUsers         bool   // if true, workers pair up (even <-> odd) and exchange

	// Registration
	SkipRegister     bool   // assume users already exist
	MaxRegisterRetry int
	RegisterMode     string // "http" or "xmpp"
	RegisterURL      string // e.g. https://host:5443/api/register
	RegisterInsecure bool   // skip TLS verify on admin API
	RegisterAPIUser  string // optional HTTP basic auth
	RegisterAPIPass  string

	// Local tuning
	RaiseFDLimit bool

	// Reporting
	ReportCSVPath string
}

func (c *Config) Defaults() {
	if c.Port == 0 {
		c.Port = 5222
	}
	if c.Domain == "" {
		c.Domain = c.Host
	}
	if c.UserPrefix == "" {
		c.UserPrefix = "lt"
	}
	if c.Password == "" {
		c.Password = "loadtest"
	}
	if c.TargetConnections == 0 {
		c.TargetConnections = 1000
	}
	if c.RampPerSec == 0 {
		c.RampPerSec = 100
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.AuthTimeout == 0 {
		c.AuthTimeout = 10 * time.Second
	}
	if c.MessageIntervalMs == 0 {
		c.MessageIntervalMs = 5000
	}
	if c.MessageBody == "" {
		c.MessageBody = "ping"
	}
	if c.MaxRegisterRetry == 0 {
		c.MaxRegisterRetry = 1
	}
	if c.RegisterMode == "" {
		c.RegisterMode = "xmpp"
	}
}

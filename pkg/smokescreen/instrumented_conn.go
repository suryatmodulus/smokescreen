package smokescreen

import (
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"net"
	"sync"
	"time"
)

type ConnExt struct {
	net.Conn `json:"-"`
	Config *Config `json:"-"`
	Role string `json:"role"`
	OutboundHost string `json:"outbound"`
	StartTime time.Time

	BytesIn int
	BytesOut int
	Wakeups int
	LastActivity time.Time

	mutex sync.Mutex `json:"-"`
}

func NewConnExt(
	conn net.Conn,
	config *Config, role,
	outboundHost string,
	startTime time.Time) (ret *ConnExt) {
	ret = &ConnExt{
		conn,
		config,
		role,
		outboundHost,
		startTime,
		0,
		0,
		0,
		time.Now(),
		sync.Mutex{},
	}

	if config.StatsServer != nil {
		config.ConnTracker.Store(ret, nil)
	}

	return
}

func (c *ConnExt) Close() error {
	if c.Config.StatsServer != nil {
		c.Config.ConnTracker.Delete(c)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	endTime := time.Now()
	duration := endTime.Sub(c.StartTime).Seconds()

	tags := []string{
		fmt.Sprintf("role:%s", c.Role),
	}

	c.Config.StatsdClient.Incr("cn.close", tags, 1)
	c.Config.StatsdClient.Histogram("cn.duration", duration, tags, 1)
	c.Config.StatsdClient.Histogram("cn.bytes_in", float64(c.BytesIn), tags, 1)
	c.Config.StatsdClient.Histogram("cn.bytes_out", float64(c.BytesOut), tags, 1)

	c.Config.Log.WithFields(logrus.Fields{
		"bytes_in":    c.BytesIn,
		"bytes_out":   c.BytesOut,
		"role":        c.Role,
		"req_host":    c.OutboundHost,
		"remote_addr": c.Conn.RemoteAddr(),
		"start_time":  c.StartTime.UTC(),
		"end_time":    endTime.UTC(),
		"duration": duration,
		"wakeups": c.Wakeups,
	}).Info("CANONICAL-PROXY-CN-CLOSE")
	return c.Conn.Close()
}

func (c *ConnExt) Read(b []byte) (n int, err error) {
	c.mutex.Lock()
	c.BytesIn += len(b)
	c.Wakeups += 1
	c.LastActivity = time.Now()
	c.mutex.Unlock()

	return c.Conn.Read(b)
}

func (c *ConnExt) Write(b []byte) (n int, err error) {
	c.mutex.Lock()
	c.BytesOut += len(b)
	c.Wakeups += 1
	c.LastActivity = time.Now()
	c.mutex.Unlock()

	return c.Conn.Write(b)
}

func (c *ConnExt) JsonStats() ([]byte, error) {
	type stats = struct {
		Id string `json:"id"`
		Role string `json:"role"`
		Rhost string `json:"rhost"`
		Created time.Time `json:"created"`
		BytesIn int `json:"bytesIn"`
		BytesOut int `json:"bytesOut"`
		Wakeups int `json:"wakeups"`
		SecondsSinceLastActivity float64 `json:"secondsSinceLastActivity"`
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	s := stats{
		Id: fmt.Sprintf("%d", &c),
		Role: c.Role,
		Rhost: c.OutboundHost,
		Created: c.StartTime,
		BytesIn: c.BytesIn,
		BytesOut: c.BytesOut,
		Wakeups: c.Wakeups,
		SecondsSinceLastActivity: time.Now().Sub(c.LastActivity).Seconds(),
	}

	return json.Marshal(s)
}

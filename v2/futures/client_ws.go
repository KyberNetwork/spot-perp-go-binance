package futures

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jpillora/backoff"
)

const (
	reconnectMinInterval = 100 * time.Millisecond
	reconnectMaxInterval = 10 * time.Second
)

var (
	ErrWsConnectionClosed = errors.New("ws error: connection closed")
	ErrWsIdAlreadySent    = errors.New("ws error: request with same id already sent")
)

type call struct {
	response []byte
	done     chan error
}

type waiter struct {
	*call
}

func (w waiter) wait(ctx context.Context) ([]byte, error) {
	select {
	case err, ok := <-w.call.done:
		if !ok {
			err = ErrWsConnectionClosed
		}
		if err != nil {
			return nil, err
		}
		return w.call.response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ClientWs define API websocket client
type ClientWs struct {
	APIKey                      string
	SecretKey                   string
	Debug                       bool
	Logger                      *log.Logger
	Conn                        *websocket.Conn
	TimeOffset                  int64
	mu                          sync.Mutex
	reconnectSignal             chan struct{}
	connectionEstablishedSignal chan struct{}
	pending                     PendingRequests
	reconnectCount              atomic.Int64
}

func (c *ClientWs) debug(format string, v ...interface{}) {
	if c.Debug {
		c.Logger.Println(fmt.Sprintf(format, v...))
	}
}

// NewClientWs init ClientWs
func NewClientWs(apiKey, secretKey string) (*ClientWs, error) {
	conn, err := WsApiInitReadWriteConn()
	if err != nil {
		return nil, err
	}

	client := &ClientWs{
		APIKey:                      apiKey,
		SecretKey:                   secretKey,
		Logger:                      log.New(os.Stderr, "Binance-golang ", log.LstdFlags),
		Conn:                        conn,
		mu:                          sync.Mutex{},
		reconnectSignal:             make(chan struct{}, 1),
		connectionEstablishedSignal: make(chan struct{}, 1),
		pending:                     NewPendingRequests(),
	}

	go client.handleReconnect()
	go client.read()

	return client, nil
}

// Write sends data into websocket connection
func (c *ClientWs) Write(id string, data []byte) (waiter, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pending.isAlreadyInList(id) {
		return waiter{}, ErrWsIdAlreadySent
	}

	if err := c.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.debug("write: unable to write message into websocket conn '%v'", err)
		return waiter{}, err
	}

	cc := c.pending.add(id)

	return waiter{cc}, nil
}

// read data from connection
func (c *ClientWs) read() {
	defer func() {
		// reading from closed connection 1000 times caused panic
		// prevent panic for any case
		if r := recover(); r != nil {
		}
	}()

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			c.debug("read: error reading message '%v'", message)
			c.reconnectSignal <- struct{}{}

			c.debug("read: wait to get connected")
			<-c.connectionEstablishedSignal

			c.debug("read: connection established")
			continue
		}

		msg := struct {
			ID string `json:"id"`
		}{}
		err = json.Unmarshal(message, &msg)
		if err != nil {
			continue
		}

		if call := c.pending.get(msg.ID); call != nil {
			call.response = message
			call.done <- nil
			close(call.done)
			c.pending.remove(msg.ID)
		}
	}
}

// handleReconnect waits for reconnect signal and starts reconnect
func (c *ClientWs) handleReconnect() {
	for range c.reconnectSignal {
		c.debug("reconnect: received signal")

		b := &backoff.Backoff{
			Min:    reconnectMinInterval,
			Max:    reconnectMaxInterval,
			Factor: 1.8,
			Jitter: false,
		}

		conn := c.startReconnect(b)

		b.Reset()

		c.mu.Lock()
		c.Conn = conn
		c.mu.Unlock()

		c.debug("reconnect: connected")
		c.connectionEstablishedSignal <- struct{}{}
	}
}

// startReconnect starts reconnect loop with increasing delay
func (c *ClientWs) startReconnect(b *backoff.Backoff) *websocket.Conn {
	for {
		c.reconnectCount.Add(1)
		conn, err := WsApiInitReadWriteConn()
		if err != nil {
			delay := b.Duration()
			c.debug("reconnect: error while reconnecting. try in %s", delay.Round(time.Millisecond))
			time.Sleep(delay)
			continue
		}

		return conn
	}
}

// GetReconnectCount returns reconnect counter value (useful for metrics outside)
func (c *ClientWs) GetReconnectCount() int64 {
	return c.reconnectCount.Load()
}

// NewPendingRequests creates request list
func NewPendingRequests() PendingRequests {
	return PendingRequests{
		mu:       sync.Mutex{},
		requests: make(map[string]*call),
	}
}

// PendingRequests state of requests that were sent/received
type PendingRequests struct {
	mu       sync.Mutex
	requests map[string]*call
}

func (l *PendingRequests) add(id string) *call {
	l.mu.Lock()
	defer l.mu.Unlock()

	c := &call{
		done: make(chan error, 1),
	}
	l.requests[id] = c
	return c
}

func (l *PendingRequests) get(id string) *call {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.requests[id]
}

func (l *PendingRequests) remove(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.requests, id)
}

func (l *PendingRequests) isAlreadyInList(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, ok := l.requests[id]
	return ok
}

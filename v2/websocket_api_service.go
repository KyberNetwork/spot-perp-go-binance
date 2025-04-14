package binance

import (
	"context"
	eJson "encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/adshao/go-binance/v2/common"
	"github.com/adshao/go-binance/v2/common/websocket"
	"github.com/google/uuid"
	gws "github.com/gorilla/websocket"
)

var (
	ErrorLogonStatusNotOK             = errors.New("error logon status")
	ErrorSubscribeUserDataStatusNotOK = errors.New("error subscribe user data status")
)

type call struct {
	request  []byte
	response []byte
	done     chan error
}

type waiter struct {
	*call
}

// wait for the response message of an ongoing Binance call.
func (w waiter) wait(ctx context.Context) ([]byte, error) {
	select {
	case err, ok := <-w.call.done:
		if !ok {
			err = websocket.ErrorWsReadConnectionTimeout
		}
		if err != nil {
			return nil, err
		}
		return w.call.response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type WsApiService struct {
	Debug   bool
	mu      sync.Mutex
	logger  *log.Logger
	conn    websocket.Connection
	pending map[string]*call

	apiKey    string
	secretKey string
}

func (c *WsApiService) debug(format string, v ...interface{}) {
	if c.Debug {
		c.logger.Println(fmt.Sprintf(format, v...))
	}
}

// NewWsApiService init WsApiService.
func NewWsApiService(apiKey, secretKey string) (*WsApiService, error) {
	conn, err := websocket.NewConnection(WsApiInitReadWriteConn, WebsocketKeepalive, WebsocketTimeoutReadWriteConnection)
	if err != nil {
		return nil, err
	}

	return &WsApiService{
		logger:    log.New(os.Stderr, "Binance-golang ", log.LstdFlags),
		conn:      conn,
		pending:   make(map[string]*call),
		apiKey:    apiKey,
		secretKey: secretKey,
	}, nil
}

// Send sends data into websocket connection
func (s *WsApiService) send(id string, data []byte) (waiter, error) {
	cc := &call{request: data, done: make(chan error, 1)}
	s.mu.Lock()
	s.pending[id] = cc
	s.mu.Unlock()

	if err := s.conn.WriteMessage(gws.TextMessage, data); err != nil {
		s.debug("send: unable to write message into websocket conn '%v'", err)
		return waiter{}, err
	}

	return waiter{cc}, nil
}

// call initiates a call to Binance server and wait for the response.
func (s *WsApiService) call(ctx context.Context, id string, msg []byte) ([]byte, error) {
	call, err := s.send(id, msg)
	if err != nil {
		s.debug("send: unable to send message into websocket conn '%v'", err)
		return nil, err
	}

	ctxTimeout, cancel := context.WithTimeout(ctx, websocket.WriteSyncWsTimeout)
	defer cancel()
	return call.wait(ctxTimeout)
}

func (s *WsApiService) callWithJsonUnmarshal(ctx context.Context, id string, msg []byte, res interface{}) error {
	data, err := s.call(ctx, id, msg)
	if err != nil {
		s.debug("call error: '%v'", err)
		return err
	}

	if err = eJson.Unmarshal(data, res); err != nil {
		s.debug("send: unable to unmarshal response data '%v'", err)
		return err
	}
	return nil
}

type SessionWsResponse struct {
	Id     string `json:"id"`
	Status int    `json:"status"`
	Result struct {
		ApiKey           string `json:"apiKey"`
		AuthorizedSince  int64  `json:"authorizedSince"`
		ConnectedSince   int64  `json:"connectedSince"`
		ReturnRateLimits bool   `json:"returnRateLimits"`
		ServerTime       int64  `json:"serverTime"`
		UserDataStream   bool   `json:"userDataStream"`
	} `json:"result"`

	// error response
	Error *common.APIError `json:"error,omitempty"`
}

func (s *WsApiService) LogonSession(ctx context.Context) (SessionWsResponse, error) {
	var ret SessionWsResponse
	uuid, err := uuid.NewUUID()
	if err != nil {
		return ret, err
	}
	req, err := websocket.CreateRequest(
		websocket.NewRequestData(uuid.String(), s.apiKey, s.secretKey, 0, common.KeyTypeEd25519),
		websocket.SessionLogonSpotWsApiMethod,
		make(map[string]interface{}),
	)
	if err != nil {
		return ret, err
	}
	if err = s.callWithJsonUnmarshal(ctx, uuid.String(), req, &ret); err != nil {
		return ret, err
	}
	return ret, nil
}

func (s *WsApiService) LogoutSession(ctx context.Context) (SessionWsResponse, error) {
	var ret SessionWsResponse
	uuid, err := uuid.NewUUID()
	if err != nil {
		return ret, err
	}
	req, err := websocket.CreateRequest(
		websocket.NewRequestData(uuid.String(), s.apiKey, s.secretKey, 0, common.KeyTypeEd25519),
		websocket.SessionLogoutSpotWsApiMethod,
		make(map[string]interface{}),
	)
	if err != nil {
		return ret, err
	}
	if err = s.callWithJsonUnmarshal(ctx, uuid.String(), req, &ret); err != nil {
		return ret, err
	}
	return ret, nil
}

func (s *WsApiService) GetSessionStatus(ctx context.Context) (SessionWsResponse, error) {
	var ret SessionWsResponse
	uuid, err := uuid.NewUUID()
	if err != nil {
		return ret, err
	}
	req, err := websocket.CreateRequest(
		websocket.NewRequestData(uuid.String(), s.apiKey, s.secretKey, 0, common.KeyTypeEd25519),
		websocket.SessionStatusSpotWsApiMethod,
		make(map[string]interface{}),
	)
	if err != nil {
		return ret, err
	}
	if err = s.callWithJsonUnmarshal(ctx, uuid.String(), req, &ret); err != nil {
		return ret, err
	}
	return ret, nil
}

type UserStreamSubscriptionResponse struct {
	Id     string `json:"id"`
	Status int    `json:"status"`

	// error response
	Error *common.APIError `json:"error,omitempty"`
}

func (s *WsApiService) subscribeUserDataStream(ctx context.Context) (UserStreamSubscriptionResponse, error) {
	var ret UserStreamSubscriptionResponse
	uuid, err := uuid.NewUUID()
	if err != nil {
		return ret, err
	}
	req, err := websocket.CreateUnsignedRequest(uuid.String(), websocket.UserDataStreamSubscribeSpotWsApiMethod, nil)
	if err != nil {
		return ret, err
	}
	if err = s.callWithJsonUnmarshal(ctx, uuid.String(), req, &ret); err != nil {
		return ret, err
	}
	return ret, nil
}

func (s *WsApiService) unsubscribeUserDataStream(ctx context.Context) (UserStreamSubscriptionResponse, error) {
	var ret UserStreamSubscriptionResponse
	uuid, err := uuid.NewUUID()
	if err != nil {
		return ret, err
	}
	req, err := websocket.CreateUnsignedRequest(uuid.String(), websocket.UserDataStreamUnsubscribeSpotWsApiMethod, nil)
	if err != nil {
		return ret, err
	}
	if err = s.callWithJsonUnmarshal(ctx, uuid.String(), req, &ret); err != nil {
		return ret, err
	}
	return ret, nil
}

type msgIDEvent struct {
	Id    string           `json:"id"`
	Event eJson.RawMessage `json:"event"`
}

// wsServe serves the websocket connection...
func (s *WsApiService) wsServe(handler WsHandler, errHandler ErrHandler) (doneC, stopC chan struct{}, err error) {
	doneC = make(chan struct{})
	stopC = make(chan struct{})

	go func() {
		defer close(doneC)
		silent := false
		go func() {
			select {
			case <-stopC:
				silent = true
			case <-doneC:
			}
			s.conn.Close()
		}()
		for {
			_, message, err := s.conn.ReadMessage()
			if err != nil {
				if !silent {
					errHandler(fmt.Errorf("read message err = %w", err))
				}
				return
			}

			var msg msgIDEvent
			if err := eJson.Unmarshal(message, &msg); err != nil {
				errHandler(fmt.Errorf("unmarshal message err = %w", err))
				return
			}

			s.mu.Lock()
			call, ok := s.pending[msg.Id]
			s.mu.Unlock()

			if ok {
				s.mu.Lock()
				delete(s.pending, msg.Id)
				s.mu.Unlock()
				call.response = message
				call.done <- nil
				close(call.done)
			} else {
				handler(msg.Event)
			}
		}
	}()
	return
}

// WsApiUserDataServe only accepts Ed25519 key type.
func WsApiUserDataServe(apiKey, secretKey string, handler WsUserDataHandler, errHandler ErrHandler) (doneC, stopC chan struct{}, err error) {
	wsHandler := func(message []byte) {
		j, err := newJSON(message)
		if err != nil {
			errHandler(err)
			return
		}

		event := new(WsUserDataEvent)

		err = eJson.Unmarshal(message, event)
		if err != nil {
			errHandler(err)
			return
		}

		switch UserDataEventType(j.Get("e").MustString()) {
		case UserDataEventTypeOutboundAccountPosition:
			err = eJson.Unmarshal(message, &event.AccountUpdate)
			if err != nil {
				errHandler(err)
				return
			}
		case UserDataEventTypeBalanceUpdate:
			err = eJson.Unmarshal(message, &event.BalanceUpdate)
			if err != nil {
				errHandler(err)
				return
			}
		case UserDataEventTypeExecutionReport:
			err = eJson.Unmarshal(message, &event.OrderUpdate)
			if err != nil {
				errHandler(err)
				return
			}
		case UserDataEventTypeListStatus:
			err = eJson.Unmarshal(message, &event.OCOUpdate)
			if err != nil {
				errHandler(err)
				return
			}
		}

		handler(event)
	}

	var wsc *WsApiService
	if wsc, err = NewWsApiService(apiKey, secretKey); err != nil {
		return
	}

	doneC, stopC, err = wsc.wsServe(wsHandler, errHandler)
	if err != nil {
		return
	}

	var (
		logonRes SessionWsResponse
		subRes   UserStreamSubscriptionResponse
	)
	if logonRes, err = wsc.LogonSession(context.Background()); err != nil {
		return
	}
	if logonRes.Status != http.StatusOK {
		err = fmt.Errorf("%w, status = %d", ErrorLogonStatusNotOK, logonRes.Status)
		return
	}

	if subRes, err = wsc.subscribeUserDataStream(context.Background()); err != nil {
		return
	}
	if subRes.Status != http.StatusOK {
		err = fmt.Errorf("%w, status = %d", ErrorSubscribeUserDataStatusNotOK, subRes.Status)
	}
	return
}

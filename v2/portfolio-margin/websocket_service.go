package portfoliomargin

import (
	"encoding/json"
	"fmt"
	"time"
)

// Endpoints
const (
	baseWsMainUrl = "wss://fstream.binance.com/pm/ws"
)

var (
	// WebsocketTimeout is an interval for sending ping/pong messages if WebsocketKeepalive is enabled
	WebsocketTimeout = time.Second * 60
	// WebsocketKeepalive enables sending ping/pong messages to check the connection stability
	WebsocketKeepalive = false
)

// getWsEndpoint return the base endpoint of the WS according the UseTestnet flag
func getWsEndpoint() string {
	return baseWsMainUrl
}

type RiskLevelType string

var (
	RiskLevelMarginCall       RiskLevelType = "MARGIN_CALL"
	RiskLevelSupplyMargin     RiskLevelType = "SUPPLY_MARGIN"
	RiskLevelReduceOnly       RiskLevelType = "REDUCE_ONLY"
	RiskLevelForceLiquidation RiskLevelType = "FORCE_LIQUIDATION"
)

// WsUserDataEvent define user data event
type WsUserDataEvent struct {
	Event           UserDataEventType `json:"e"`
	FS              string            `json:"fs"`
	Time            int64             `json:"E"`
	TransactionTime int64             `json:"T"`
	Alias           string            `json:"i"`
	UpdatedId       int64             `json:"U"`
	LastUpdateTime  int64             `json:"u"`

	FutureAccountUpdate       WsFutureAccountUpdate       `json:"a"`
	FutureOrderUpdate         WsFutureOrderTradeUpdate    `json:"o"`
	FutureAccountConfigUpdate WsFutureAccountConfigUpdate `json:"ac"`
	MarginAccountUpdate       []WsMarginAccountUpdate     `json:"B"`
}

// WsMarginAccountUpdate define account update
type WsMarginAccountUpdate struct {
	Asset  string `json:"a"`
	Free   string `json:"f"`
	Locked string `json:"l"`
}

// WsFutureAccountUpdate define account update
type WsFutureAccountUpdate struct {
	Reason    UserDataEventReasonType `json:"m"`
	Balances  []WsFutureBalance       `json:"B"`
	Positions []WsFuturePosition      `json:"P"`
}

// WsFutureBalance define balance
type WsFutureBalance struct {
	Asset              string `json:"a"`
	Balance            string `json:"wb"`
	CrossWalletBalance string `json:"cw"`
	ChangeBalance      string `json:"bc"`
}

// WsFuturePosition define position
type WsFuturePosition struct {
	Symbol              string           `json:"s"`
	Side                PositionSideType `json:"ps"`
	Amount              string           `json:"pa"`
	EntryPrice          string           `json:"ep"`
	UnrealizedPnL       string           `json:"up"`
	AccumulatedRealized string           `json:"cr"`
	BreakEvenPrice      string           `json:"bep"`
}

// WsFutureOrderTradeUpdate define order trade update
type WsFutureOrderTradeUpdate struct {
	Symbol               string                  `json:"s"`
	ClientOrderID        string                  `json:"c"`
	Side                 SideType                `json:"S"`
	Type                 OrderType               `json:"o"`
	TimeInForce          TimeInForceType         `json:"f"`
	OriginalQty          string                  `json:"q"`
	OriginalPrice        string                  `json:"p"`
	AveragePrice         string                  `json:"ap"`
	StopPrice            string                  `json:"sp"`
	ExecutionType        OrderExecutionType      `json:"x"`
	Status               OrderStatusType         `json:"X"`
	ID                   int64                   `json:"i"`
	LastFilledQty        string                  `json:"l"`
	AccumulatedFilledQty string                  `json:"z"`
	LastFilledPrice      string                  `json:"L"`
	CommissionAsset      string                  `json:"N"`
	Commission           string                  `json:"n"`
	TradeTime            int64                   `json:"T"`
	TradeID              int64                   `json:"t"`
	BidsNotional         string                  `json:"b"`
	AsksNotional         string                  `json:"a"`
	IsMaker              bool                    `json:"m"`
	IsReduceOnly         bool                    `json:"R"`
	PositionSide         PositionSideType        `json:"ps"`
	RealizedPnL          string                  `json:"rp"`
	Strategy             StrategyType            `json:"st"`
	StrategyID           int64                   `json:"si"`
	STP                  SelfTradePreventionMode `json:"V"`
	Gtd                  int                     `json:"gtd"`
}

// WsFutureAccountConfigUpdate define account config update
type WsFutureAccountConfigUpdate struct {
	Symbol   string `json:"s"`
	Leverage int64  `json:"l"`
}

// WsUserDataHandler handle WsUserDataEvent
type WsUserDataHandler func(event interface{})

// WsRiskLevelChangeEvent define risk level change event
type WsRiskLevelChangeEvent struct {
	Event             UserDataEventType `json:"e"`
	Time              int64             `json:"E"`
	UniMMR            string            `json:"u"`
	RiskLevel         RiskLevelType     `json:"s"`
	AccountEquityUSD  string            `json:"eq"` // account equity in USD value
	AccountEquity     string            `json:"ae"` // actual equity without collateral rate in USD value
	MaintenanceMargin string            `json:"mm"` // total maintenance margin in USD value
}

// WsOpenOrderLossEvent define open order loss event
type WsOpenOrderLossEvent struct {
	Event  UserDataEventType `json:"e"`
	Time   int64             `json:"E"`
	Orders []struct {
		Asset  string `json:"a"`
		Amount string `json:"o"`
	} `json:"O"`
}

// WsLiabilityUpdateEvent define user liability
type WsLiabilityUpdateEvent struct {
	Event          UserDataEventType `json:"e"`
	Time           int64             `json:"E"`
	Asset          string            `json:"a"`
	LiabilityType  string            `json:"t"`
	TransactionID  int64             `json:"tx"`
	Principal      string            `json:"p"`
	Interest       string            `json:"i"`
	TotalLiability string            `json:"l"`
}

// WsMarginBalanceUpdateEvent define margin balance update
type WsMarginBalanceUpdateEvent struct {
	Event        UserDataEventType `json:"e"`
	Time         int64             `json:"E"`
	Asset        string            `json:"a"`
	BalanceDelta string            `json:"d"`
	UpdatedId    int64             `json:"U"`
	ClearTime    int64             `json:"T"`
}

// WsUserDataServe serve user data handler with listen key
func WsUserDataServe(listenKey string, handler WsUserDataHandler, errHandler ErrHandler) (doneC, stopC chan struct{}, err error) {
	endpoint := fmt.Sprintf("%s/%s", getWsEndpoint(), listenKey)
	cfg := newWsConfig(endpoint)
	wsHandler := func(message []byte) {
		j, err := newJSON(message)
		if err != nil {
			errHandler(err)
			return
		}
		switch j.Get("e").MustString() {
		case "riskLevelChange":
			event := new(WsRiskLevelChangeEvent)
			if err = json.Unmarshal(message, event); err != nil {
				errHandler(err)
				return
			}
			handler(event)
		case "openOrderLoss":
			event := new(WsOpenOrderLossEvent)
			if err = json.Unmarshal(message, event); err != nil {
				errHandler(err)
				return
			}
			handler(event)
		case "liabilityChange":
			event := new(WsLiabilityUpdateEvent)
			if err = json.Unmarshal(message, event); err != nil {
				errHandler(err)
				return
			}
			handler(event)
		case "balanceUpdate":
			event := new(WsMarginBalanceUpdateEvent)
			if err = json.Unmarshal(message, event); err != nil {
				errHandler(err)
				return
			}
			handler(event)
		default:
			// var rawData interface{}
			// json.Unmarshal(message, &rawData)
			// fmt.Printf("\nraw data %+v\n", rawData)
			event := new(WsUserDataEvent)
			err = json.Unmarshal(message, event)
			if err != nil {
				errHandler(err)
				return
			}
			handler(event)
		}
	}
	return wsServe(cfg, wsHandler, errHandler)
}

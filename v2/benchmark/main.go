package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/urfave/cli/v2"
	"golang.org/x/exp/rand"
	"golang.org/x/sync/errgroup"
)

const (
	orderNum = 50

	binanceApiKeyFlag    = "binance-api-key"
	binanceSecretKeyFlag = "binance-secret-key"
	outputFolderFlag     = "output-folder"
)

func main() {
	app := cli.NewApp()
	app.Name = "Future order benchmark"
	app.Action = run
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    binanceApiKeyFlag,
			EnvVars: []string{"BINANCE_API_KEY"},
		},
		&cli.StringFlag{
			Name:    binanceSecretKeyFlag,
			EnvVars: []string{"BINANCE_SECRET_KEY"},
		},
		&cli.StringFlag{
			Name:    outputFolderFlag,
			EnvVars: []string{"OUTPUT_FOLDER"},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Panic(err)
	}
}

func run(c *cli.Context) error {
	l := setupLogger()
	l.Infow("Start running benchmark...")

	apiKey, secretKey := c.String(binanceApiKeyFlag), c.String(binanceSecretKeyFlag)

	restClient := futures.NewClient(apiKey, secretKey)
	wsClient, err := futures.NewOrderPlaceWsService(apiKey, secretKey)
	if err != nil {
		l.Errorw("Cannot init wsClient", "err", err)
		return err
	}

	// Prepare for CSV
	header := []string{"symbol", "qty", "price", "side", "tif", "ws_latency", "rest_latency"}
	data := [][]string{}

	// Setup test
	mappedExInfo, err := getFutureExInfo(restClient, l)
	if err != nil {
		l.Errorw("Failed to get future exchange info", "err", err)
		return err
	}

	tickers, err := restClient.NewListPriceChangeStatsService().Do(context.Background())
	if err != nil {
		l.Errorw("Failed to get binance ticker", "err", err)
		return err
	}

	serverTimeDiff, err := getFutureServerTimeDiff(restClient)
	if err != nil {
		l.Errorw("Cannot getFutureServerTimeDiff", "err", err)
		return err
	}

	tests := setupFutureOrderTest(mappedExInfo, tickers, orderNum)
	l.Infow("Place future order tests", "data", tests)

	for _, test := range tests {
		var (
			now                          = time.Now().UnixMilli()
			eg                           errgroup.Group
			wsUpdateTime, restUpdateTime int64
		)

		// place WS order
		eg.Go(func() error {
			req := futures.NewOrderPlaceWsRequest().
				Symbol(test.Symbol).
				Side(futures.SideTypeBuy).
				Type(futures.OrderTypeLimit).
				Price(FloatToString(test.Price)).
				Quantity(FloatToString(test.Qty)).
				TimeInForce(futures.TimeInForceTypeIOC).
				NewOrderResponseType(futures.NewOrderRespTypeRESULT)
			order, err := wsClient.Do(context.Background(), req)
			if err != nil {
				l.Errorw("Failed to place ws order", "err", err)
				return err
			}
			wsUpdateTime = order.UpdateTime
			return nil
		})

		// place rest API order
		eg.Go(func() error {
			order, err := restClient.NewCreateOrderService().
				Symbol(test.Symbol).
				Side(futures.SideTypeBuy).
				Type(futures.OrderTypeLimit).
				TimeInForce(futures.TimeInForceTypeIOC).
				Price(FloatToString(test.Price)).
				Quantity(FloatToString(test.Qty)).
				NewOrderResponseType(futures.NewOrderRespTypeRESULT).
				Do(context.Background())
			if err != nil {
				l.Errorw("Failed to place rest order", "err", err)
				return err
			}
			restUpdateTime = order.UpdateTime
			return nil
		})
		if err := eg.Wait(); err != nil {
			l.Errorw("Failed to place order", "err", err)
		} else {
			// "symbol", "qty", "price", "side", "tif", "ws_latency", "rest_latency"
			data = append(data, []string{
				test.Symbol, FloatToString(test.Qty), FloatToString(test.Price), "BUY", "IOC",
				IntToString(wsUpdateTime - now - int64(serverTimeDiff)),
				IntToString(restUpdateTime - now - int64(serverTimeDiff)),
			})

			time.Sleep(time.Duration(rand.Intn(1000)+1) * time.Millisecond)
		}
	}

	if err := WriteCSV(c.String(outputFolderFlag), header, data); err != nil {
		l.Errorw("Failed to WriteCSV", "err", err)
		return err
	}

	l.Info("CSV file written successfully")
	return nil
}

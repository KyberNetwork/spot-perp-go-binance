package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/adshao/go-binance/v2/futures"
)

func setupLogger() *zap.SugaredLogger {
	pConf := zap.NewProductionEncoderConfig()
	pConf.EncodeTime = zapcore.ISO8601TimeEncoder
	encoder := zapcore.NewConsoleEncoder(pConf)
	level := zap.NewAtomicLevelAt(zap.DebugLevel)
	l := zap.New(zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level), zap.AddCaller())
	zap.ReplaceGlobals(l)
	return zap.S()
}

type placeOrderParam struct {
	Symbol string
	Price  float64
	Qty    float64
}

type exchangeInfo struct {
	PricePrecision int
	QtyPrecision   int
	MinNotional    float64
}

func getFutureExInfo(
	client *futures.Client, l *zap.SugaredLogger,
) (map[string]exchangeInfo, error) {
	exInfo, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		l.Errorw("Failed to get future exchange info", "err", err)
		return nil, err
	}

	mappedExInfo := make(map[string]exchangeInfo)
	var (
		pricePrecision, qtyPrecision int
		minNotional                  float64
	)
	for _, s := range exInfo.Symbols {
		if s.QuoteAsset != "USDT" || s.Status != "TRADING" {
			continue
		}
		for _, f := range s.Filters {
			switch f["filterType"].(string) {
			case "PRICE_FILTER":
				_, pricePrecision, err = GetPrecision(f["tickSize"].(string))
				if err != nil {
					l.Errorw("Failed to get pricePrecision", "err", err)
					return nil, err
				}
			case "LOT_SIZE":
				_, qtyPrecision, err = GetPrecision(f["stepSize"].(string))
				if err != nil {
					l.Errorw("Failed to get qtyPrecision", "err", err)
					return nil, err
				}
			case "MIN_NOTIONAL":
				minNotional, err = strconv.ParseFloat(f["notional"].(string), 64)
				if err != nil {
					l.Errorw("Failed to get minMotional", "err", err)
					return nil, err
				}
			}
			mappedExInfo[s.Symbol] = exchangeInfo{
				PricePrecision: pricePrecision,
				QtyPrecision:   qtyPrecision,
				MinNotional:    minNotional,
			}
		}
	}
	return mappedExInfo, nil
}

func setupFutureOrderTest(
	mappedExInfo map[string]exchangeInfo,
	tickers []*futures.PriceChangeStats,
	testSize int,
) []placeOrderParam {
	res := make([]placeOrderParam, 0, testSize)
	count := 0
	for _, ticker := range tickers {
		if count >= testSize {
			break
		}
		// place BUY order with price = 0.9 * lastPrice, qty = 3 * minNotional
		if exInfo, ok := mappedExInfo[ticker.Symbol]; ok {
			price := RoundDown(0.9*StringToFloat(ticker.LastPrice), exInfo.PricePrecision)
			if price == 0 {
				continue
			}
			qty := RoundDown(3*exInfo.MinNotional/price, exInfo.QtyPrecision)
			if qty == 0 {
				continue
			}
			res = append(res, placeOrderParam{
				Symbol: ticker.Symbol,
				Price:  price,
				Qty:    qty,
			})
			count += 1
		}
	}
	return res
}

func WriteCSV(path string, header []string, data [][]string) error {
	// Create a new CSV file
	file, err := os.Create(fmt.Sprintf("%s/benchmark_%d.csv", path, time.Now().Unix()))
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write(header); err != nil {
		return err
	}

	for _, record := range data {
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}

func getFutureServerTimeDiff(client *futures.Client) (float64, error) {
	diffs := make([]float64, 0)
	for i := 0; i < 3; i++ {
		startTime := time.Now().UnixMilli()
		serverTime, err := client.NewServerTimeService().Do(context.Background())
		finishTime := time.Now().UnixMilli()
		if err != nil {
			return 0, err
		}
		diffs = append(diffs, float64(serverTime-(startTime+finishTime)/2))
	}

	return Mean(diffs), nil
}

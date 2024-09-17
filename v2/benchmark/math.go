package main

import (
	"errors"
	"math"
	"path"
	"runtime"
	"strconv"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// RoundingNumberDown ...
func RoundDown(n float64, precision int) float64 {
	rounding := math.Pow10(precision)
	return math.Floor(n*rounding) / rounding
}

func StringToFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		pc, file, line, _ := runtime.Caller(1)
		zap.S().Errorw(
			"Failed to ParseStringToFloat!",
			"err", err, "raw", s,
			"funcName", runtime.FuncForPC(pc).Name(),
			"file", path.Base(file), "line", line,
		)
	}
	return f
}

func FloatToString(f float64) string {
	df := decimal.NewFromFloat(f)
	return df.String()
}

func IntToString(d int64) string {
	return strconv.FormatInt(d, 10)
}

func GetPrecision(precisionString string) (float64, int, error) {
	f, err := strconv.ParseFloat(precisionString, 64)
	if err != nil {
		return 0, 0, err
	}
	if f == 0 {
		return 0, 0, errors.New("precision string is zero") // nolint: goerr113
	}
	precision := math.Log10(1 / f)
	return f, int(math.Round(precision)), nil
}

func Mean(values []float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}

	res := 0.0
	for _, v := range values {
		res += v / float64(n)
	}
	return res
}

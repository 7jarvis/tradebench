// Package builders produces valid request payloads with sensible defaults.
// Tests change only the field under test, which keeps intent visible.
package builders

import (
	"strconv"

	"github.com/7jarvis/tradebench/tests/internal/models"
)

func MarketBuy(symbol string, qty int) models.OrderRequest {
	return order(symbol, "buy", "market", qty, nil)
}

func MarketSell(symbol string, qty int) models.OrderRequest {
	return order(symbol, "sell", "market", qty, nil)
}

func LimitBuy(symbol string, qty int, limitPrice string) models.OrderRequest {
	return order(symbol, "buy", "limit", qty, &limitPrice)
}

func LimitSell(symbol string, qty int, limitPrice string) models.OrderRequest {
	return order(symbol, "sell", "limit", qty, &limitPrice)
}

func order(symbol, side, typ string, qty int, limit *string) models.OrderRequest {
	return models.OrderRequest{
		Symbol: symbol, Side: side, Type: typ, Qty: strconv.Itoa(qty), LimitPrice: limit,
	}
}

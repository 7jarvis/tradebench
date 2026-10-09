package broker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/7jarvis/tradebench/service/internal/money"
)

// Service implements the trading rules.
//
// Concurrency: every money/position change happens while the account row is
// locked (SELECT ... FOR UPDATE). Locks are always taken in the same order —
// prices -> orders -> accounts -> positions — so concurrent requests cannot
// deadlock and cannot spend the same buying power twice.
type Service struct {
	db    *sql.DB
	topic string
	now   func() time.Time
}

func NewService(db *sql.DB, topic string) *Service {
	return &Service{
		db:    db,
		topic: topic,
		// Postgres stores microseconds; truncate so API responses equal stored values.
		now: func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) },
	}
}

// ---------- accounts ----------

func (s *Service) OpenAccount(ctx context.Context, in OpenAccountInput) (Account, error) {
	name, err := validateOwnerName(in.OwnerName)
	if err != nil {
		return Account{}, err
	}
	a := accountRow{id: newID(), owner: name, status: AccountActive, created: s.now()}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO accounts (id, owner_name, status, created_at) VALUES ($1, $2, $3, $4)`,
		a.id, a.owner, a.status, a.created)
	if err != nil {
		return Account{}, err
	}
	return a.toAccount(), nil
}

func (s *Service) GetAccount(ctx context.Context, id string) (Account, error) {
	a, err := getAccount(ctx, s.db, id, false)
	if err != nil {
		return Account{}, err
	}
	return a.toAccount(), nil
}

func (s *Service) Deposit(ctx context.Context, id, amount string) (Account, error) {
	amt, err := validateAmount("amount", amount, maxDeposit)
	if err != nil {
		return Account{}, err
	}
	var out Account
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		a, err := getAccount(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a.status != AccountActive {
			return ErrAccountInactive
		}
		a.cash += amt
		if err := writeAccount(ctx, tx, a); err != nil {
			return err
		}
		out = a.toAccount()
		return nil
	})
	return out, err
}

// CloseAccount cancels open orders and marks the account closed. Idempotent.
func (s *Service) CloseAccount(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return ErrAccountNotFound
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		open, err := queryOrders(ctx, tx,
			`SELECT `+orderCols+` FROM orders WHERE account_id = $1 AND status = 'new' ORDER BY created_at, id FOR UPDATE`, id)
		if err != nil {
			return err
		}
		a, err := getAccount(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a.status == AccountClosed {
			return nil
		}
		for _, o := range open {
			if err := s.cancelLocked(ctx, tx, &a, o); err != nil {
				return err
			}
		}
		a.status = AccountClosed
		return writeAccount(ctx, tx, a)
	})
}

// ---------- orders ----------

func (s *Service) PlaceOrder(ctx context.Context, accountID string, in PlaceOrderInput) (Order, error) {
	v, err := validateOrder(in)
	if err != nil {
		return Order{}, err
	}

	var out Order
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		a, err := getAccount(ctx, tx, accountID, true)
		if err != nil {
			return err
		}
		if a.status != AccountActive {
			return ErrAccountInactive
		}

		var price money.Cents
		err = tx.QueryRowContext(ctx, `SELECT price_cents FROM prices WHERE symbol = $1`, v.symbol).Scan(&price)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknownSymbol
		}
		if err != nil {
			return err
		}

		if v.clientOrderID != "" {
			var exists bool
			err = tx.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM orders WHERE account_id = $1 AND client_order_id = $2)`,
				a.id, v.clientOrderID).Scan(&exists)
			if err != nil {
				return err
			}
			if exists {
				return ErrDuplicateClient
			}
		}

		now := s.now()
		o := Order{
			ID: newID(), ClientOrderID: v.clientOrderID, AccountID: a.id,
			Symbol: v.symbol, Side: v.side, Type: v.typ, Qty: Qty(v.qty), LimitPrice: v.limitPrice,
			Status: OrderNew, CreatedAt: now, UpdatedAt: now,
		}
		if o.ClientOrderID == "" {
			o.ClientOrderID = o.ID
		}

		marketable := isMarketable(o, price)
		available := a.cash - a.reserved

		switch o.Side {
		case SideBuy:
			if marketable {
				if price.Mul(v.qty) > available {
					return ErrBuyingPower
				}
			} else {
				reserve := v.limitPrice.Mul(v.qty)
				if reserve > available {
					return ErrBuyingPower
				}
				a.reserved += reserve
			}
		case SideSell:
			var held, pendingSell int64
			err = tx.QueryRowContext(ctx,
				`SELECT COALESCE((SELECT qty FROM positions WHERE account_id = $1 AND symbol = $2), 0),
				        COALESCE((SELECT SUM(qty) FROM orders
				                  WHERE account_id = $1 AND symbol = $2 AND side = 'sell' AND status = 'new'), 0)`,
				a.id, o.Symbol).Scan(&held, &pendingSell)
			if err != nil {
				return err
			}
			if v.qty > held-pendingSell {
				return ErrInsufficientQty
			}
		}

		_, err = tx.ExecContext(ctx,
			`INSERT INTO orders (`+orderCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			o.ID, o.ClientOrderID, o.AccountID, o.Symbol, o.Side, o.Type, int64(o.Qty), nullCents(o.LimitPrice),
			o.Status, int64(o.FilledQty), nullCents(o.FilledAvgPrice), o.CreatedAt, o.UpdatedAt, o.FilledAt, o.CanceledAt)
		if err != nil {
			return err
		}
		if err := s.emit(ctx, tx, o, "new", nil, nil); err != nil {
			return err
		}
		if marketable {
			if err := s.fillLocked(ctx, tx, &a, &o, price, false); err != nil {
				return err
			}
		}
		if err := writeAccount(ctx, tx, a); err != nil {
			return err
		}
		out = o
		return nil
	})
	return out, err
}

func (s *Service) GetOrder(ctx context.Context, accountID, orderID string) (Order, error) {
	if _, err := getAccount(ctx, s.db, accountID, false); err != nil {
		return Order{}, err
	}
	if !uuidRe.MatchString(orderID) {
		return Order{}, ErrOrderNotFound
	}
	o, err := scanOrder(s.db.QueryRowContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE id = $1 AND account_id = $2`, orderID, accountID))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	return o, err
}

// ListOrders returns orders newest first. status: open (default) | closed | all.
func (s *Service) ListOrders(ctx context.Context, accountID, status string) ([]Order, error) {
	if _, err := getAccount(ctx, s.db, accountID, false); err != nil {
		return nil, err
	}
	filter := ""
	switch status {
	case "", "open":
		filter = `AND status = 'new'`
	case "closed":
		filter = `AND status <> 'new'`
	case "all":
	default:
		return nil, invalid("status", "status must be one of: open, closed, all")
	}
	return queryOrders(ctx, s.db,
		`SELECT `+orderCols+` FROM orders WHERE account_id = $1 `+filter+` ORDER BY created_at DESC, id`, accountID)
}

func (s *Service) CancelOrder(ctx context.Context, accountID, orderID string) error {
	if _, err := getAccount(ctx, s.db, accountID, false); err != nil {
		return err
	}
	if !uuidRe.MatchString(orderID) {
		return ErrOrderNotFound
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		o, err := scanOrder(tx.QueryRowContext(ctx,
			`SELECT `+orderCols+` FROM orders WHERE id = $1 AND account_id = $2 FOR UPDATE`, orderID, accountID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOrderNotFound
		}
		if err != nil {
			return err
		}
		if o.Status != OrderNew {
			return ErrNotCancelable
		}
		a, err := getAccount(ctx, tx, accountID, true)
		if err != nil {
			return err
		}
		if err := s.cancelLocked(ctx, tx, &a, o); err != nil {
			return err
		}
		return writeAccount(ctx, tx, a)
	})
}

// ---------- positions ----------

func (s *Service) ListPositions(ctx context.Context, accountID string) ([]Position, error) {
	if _, err := getAccount(ctx, s.db, accountID, false); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.symbol, p.qty, p.avg_entry_price_cents, pr.price_cents
		   FROM positions p JOIN prices pr USING (symbol)
		  WHERE p.account_id = $1 ORDER BY p.symbol`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Position{}
	for rows.Next() {
		var sym string
		var qty int64
		var avg, price money.Cents
		if err := rows.Scan(&sym, &qty, &avg, &price); err != nil {
			return nil, err
		}
		out = append(out, toPosition(sym, qty, avg, price))
	}
	return out, rows.Err()
}

func (s *Service) GetPosition(ctx context.Context, accountID, symbol string) (Position, error) {
	if _, err := getAccount(ctx, s.db, accountID, false); err != nil {
		return Position{}, err
	}
	var qty int64
	var avg, price money.Cents
	err := s.db.QueryRowContext(ctx,
		`SELECT p.qty, p.avg_entry_price_cents, pr.price_cents
		   FROM positions p JOIN prices pr USING (symbol)
		  WHERE p.account_id = $1 AND p.symbol = $2`, accountID, symbol).Scan(&qty, &avg, &price)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, ErrPositionNotFound
	}
	if err != nil {
		return Position{}, err
	}
	return toPosition(symbol, qty, avg, price), nil
}

// ---------- sandbox market data ----------

func (s *Service) GetPrice(ctx context.Context, symbol string) (Price, error) {
	p := Price{Symbol: symbol}
	err := s.db.QueryRowContext(ctx,
		`SELECT price_cents, updated_at FROM prices WHERE symbol = $1`, symbol).Scan(&p.Price, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Price{}, ErrSymbolNotFound
	}
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, err
}

// SetPrice creates or updates a symbol's price and fills every open limit
// order the new price crosses, at the new price.
func (s *Service) SetPrice(ctx context.Context, symbol, price string) (Price, error) {
	if !symbolRe.MatchString(symbol) {
		return Price{}, invalid("symbol", "symbol must be 1-10 uppercase letters")
	}
	p, err := validateAmount("price", price, maxPrice)
	if err != nil {
		return Price{}, err
	}

	var out Price
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		_, err := tx.ExecContext(ctx,
			`INSERT INTO prices (symbol, price_cents, updated_at) VALUES ($1, $2, $3)
			 ON CONFLICT (symbol) DO UPDATE SET price_cents = EXCLUDED.price_cents, updated_at = EXCLUDED.updated_at`,
			symbol, int64(p), now)
		if err != nil {
			return err
		}

		crossed, err := queryOrders(ctx, tx,
			`SELECT `+orderCols+` FROM orders
			  WHERE symbol = $1 AND status = 'new'
			    AND ((side = 'buy' AND limit_price_cents >= $2) OR (side = 'sell' AND limit_price_cents <= $2))
			  ORDER BY created_at, id FOR UPDATE`, symbol, int64(p))
		if err != nil {
			return err
		}
		for i := range crossed {
			a, err := getAccount(ctx, tx, crossed[i].AccountID, true)
			if err != nil {
				return err
			}
			if err := s.fillLocked(ctx, tx, &a, &crossed[i], p, true); err != nil {
				return err
			}
			if err := writeAccount(ctx, tx, a); err != nil {
				return err
			}
		}
		out = Price{Symbol: symbol, Price: p, UpdatedAt: now}
		return nil
	})
	return out, err
}

// ---------- internals ----------

func isMarketable(o Order, price money.Cents) bool {
	if o.Type == TypeMarket {
		return true
	}
	if o.Side == SideBuy {
		return price <= *o.LimitPrice
	}
	return price >= *o.LimitPrice
}

// fillLocked executes the whole order at price. The caller holds the order
// and account locks and persists the account afterwards.
// releaseReserve is true for resting buy limit orders whose cash was reserved.
func (s *Service) fillLocked(ctx context.Context, tx *sql.Tx, a *accountRow, o *Order, price money.Cents, releaseReserve bool) error {
	qty := int64(o.Qty)

	var posQty int64
	var posAvg money.Cents
	err := tx.QueryRowContext(ctx,
		`SELECT qty, avg_entry_price_cents FROM positions WHERE account_id = $1 AND symbol = $2 FOR UPDATE`,
		a.id, o.Symbol).Scan(&posQty, &posAvg)
	hasPos := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	switch o.Side {
	case SideBuy:
		if releaseReserve {
			a.reserved -= o.LimitPrice.Mul(qty)
		}
		a.cash -= price.Mul(qty)
		newQty := posQty + qty
		// Weighted average entry price, rounded half up to the cent.
		total := int64(posAvg)*posQty + int64(price)*qty
		newAvg := money.Cents((total + newQty/2) / newQty)
		if hasPos {
			_, err = tx.ExecContext(ctx,
				`UPDATE positions SET qty = $3, avg_entry_price_cents = $4 WHERE account_id = $1 AND symbol = $2`,
				a.id, o.Symbol, newQty, int64(newAvg))
		} else {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO positions (account_id, symbol, qty, avg_entry_price_cents) VALUES ($1, $2, $3, $4)`,
				a.id, o.Symbol, newQty, int64(newAvg))
		}
	case SideSell:
		if !hasPos || posQty < qty {
			return fmt.Errorf("invariant violated: sell %d %s but position is %d", qty, o.Symbol, posQty)
		}
		a.cash += price.Mul(qty)
		if posQty == qty {
			_, err = tx.ExecContext(ctx, `DELETE FROM positions WHERE account_id = $1 AND symbol = $2`, a.id, o.Symbol)
		} else {
			_, err = tx.ExecContext(ctx,
				`UPDATE positions SET qty = $3 WHERE account_id = $1 AND symbol = $2`, a.id, o.Symbol, posQty-qty)
		}
	}
	if err != nil {
		return err
	}
	if a.cash < 0 || a.reserved < 0 || a.reserved > a.cash {
		return fmt.Errorf("invariant violated: cash=%s reserved=%s", a.cash, a.reserved)
	}

	now := s.now()
	p := price
	o.Status, o.FilledQty, o.FilledAvgPrice, o.FilledAt, o.UpdatedAt = OrderFilled, Qty(qty), &p, &now, now
	_, err = tx.ExecContext(ctx,
		`UPDATE orders SET status = $2, filled_qty = $3, filled_avg_price_cents = $4, filled_at = $5, updated_at = $5 WHERE id = $1`,
		o.ID, o.Status, qty, int64(price), now)
	if err != nil {
		return err
	}
	q := o.Qty
	return s.emit(ctx, tx, *o, "fill", &p, &q)
}

// cancelLocked cancels an open order and releases its reservation on a.
func (s *Service) cancelLocked(ctx context.Context, tx *sql.Tx, a *accountRow, o Order) error {
	if o.Side == SideBuy && o.LimitPrice != nil {
		a.reserved -= o.LimitPrice.Mul(int64(o.Qty))
	}
	now := s.now()
	o.Status, o.CanceledAt, o.UpdatedAt = OrderCanceled, &now, now
	_, err := tx.ExecContext(ctx,
		`UPDATE orders SET status = $2, canceled_at = $3, updated_at = $3 WHERE id = $1`, o.ID, o.Status, now)
	if err != nil {
		return err
	}
	return s.emit(ctx, tx, o, "canceled", nil, nil)
}

func (s *Service) emit(ctx context.Context, tx *sql.Tx, o Order, event string, price *money.Cents, qty *Qty) error {
	payload, err := json.Marshal(TradeUpdate{Event: event, Timestamp: s.now(), Price: price, Qty: qty, Order: o})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3)`, s.topic, o.AccountID, string(payload))
	return err
}

func (s *Service) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func toPosition(symbol string, qty int64, avg, price money.Cents) Position {
	cost, value := avg.Mul(qty), price.Mul(qty)
	return Position{
		Symbol: symbol, Qty: Qty(qty), AvgEntryPrice: avg, CostBasis: cost,
		CurrentPrice: price, MarketValue: value, UnrealizedPL: value - cost,
	}
}

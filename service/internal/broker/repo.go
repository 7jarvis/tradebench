package broker

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/7jarvis/tradebench/service/internal/money"
)

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type scanner interface{ Scan(dest ...any) error }

type accountRow struct {
	id, owner, status string
	cash, reserved    money.Cents
	created           time.Time
}

func (a accountRow) toAccount() Account {
	return Account{
		ID: a.id, OwnerName: a.owner, Status: a.status, Currency: Currency,
		Cash: a.cash, BuyingPower: a.cash - a.reserved, CreatedAt: a.created,
	}
}

func getAccount(ctx context.Context, q querier, id string, forUpdate bool) (accountRow, error) {
	if !uuidRe.MatchString(id) {
		return accountRow{}, ErrAccountNotFound
	}
	query := `SELECT id, owner_name, status, cash_cents, reserved_cents, created_at FROM accounts WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var a accountRow
	err := q.QueryRowContext(ctx, query, id).Scan(&a.id, &a.owner, &a.status, &a.cash, &a.reserved, &a.created)
	if errors.Is(err, sql.ErrNoRows) {
		return accountRow{}, ErrAccountNotFound
	}
	a.created = a.created.UTC()
	return a, err
}

func writeAccount(ctx context.Context, q querier, a accountRow) error {
	_, err := q.ExecContext(ctx,
		`UPDATE accounts SET status = $2, cash_cents = $3, reserved_cents = $4 WHERE id = $1`,
		a.id, a.status, int64(a.cash), int64(a.reserved))
	return err
}

const orderCols = `id, client_order_id, account_id, symbol, side, type, qty, limit_price_cents, status,
	filled_qty, filled_avg_price_cents, created_at, updated_at, filled_at, canceled_at`

func scanOrder(row scanner) (Order, error) {
	var (
		o                    Order
		qty, filledQty       int64
		limit, avg           sql.NullInt64
		filledAt, canceledAt sql.NullTime
	)
	err := row.Scan(&o.ID, &o.ClientOrderID, &o.AccountID, &o.Symbol, &o.Side, &o.Type, &qty, &limit, &o.Status,
		&filledQty, &avg, &o.CreatedAt, &o.UpdatedAt, &filledAt, &canceledAt)
	if err != nil {
		return Order{}, err
	}
	o.Qty, o.FilledQty = Qty(qty), Qty(filledQty)
	o.LimitPrice, o.FilledAvgPrice = centsPtr(limit), centsPtr(avg)
	o.CreatedAt, o.UpdatedAt = o.CreatedAt.UTC(), o.UpdatedAt.UTC()
	o.FilledAt, o.CanceledAt = timePtr(filledAt), timePtr(canceledAt)
	return o, nil
}

func queryOrders(ctx context.Context, q querier, query string, args ...any) ([]Order, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func centsPtr(v sql.NullInt64) *money.Cents {
	if !v.Valid {
		return nil
	}
	c := money.Cents(v.Int64)
	return &c
}

func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func nullCents(c *money.Cents) sql.NullInt64 {
	if c == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*c), Valid: true}
}

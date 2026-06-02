package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type TxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func WithTx(ctx context.Context, db TxBeginner, fn func(Repositories) error) error {
	return WithTxOptions(ctx, db, pgx.TxOptions{}, fn)
}

func WithTxOptions(ctx context.Context, db TxBeginner, options pgx.TxOptions, fn func(Repositories) error) error {
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return err
	}

	if err := fn(New(tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	return tx.Commit(ctx)
}

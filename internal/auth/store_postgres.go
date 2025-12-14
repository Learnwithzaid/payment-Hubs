package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresClientStore struct {
	Pool *pgxpool.Pool
}

func (s *PostgresClientStore) GetClient(ctx context.Context, clientID string) (*Client, error) {
	if s.Pool == nil {
		return nil, errors.New("missing pool")
	}

	var c Client
	var scopes []string
	err := s.Pool.QueryRow(ctx, `SELECT client_id, secret_hash, scopes FROM oauth_clients WHERE client_id = $1`, clientID).Scan(&c.ID, &c.SecretHash, &scopes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClientNotFound
		}
		return nil, err
	}
	c.Scopes = scopes
	return &c, nil
}

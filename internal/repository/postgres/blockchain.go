package postgres

import (
	"context"
	"database/sql"
	"kyd/internal/domain"
	"kyd/pkg/errors"

	"github.com/jmoiron/sqlx"
)

type BlockchainNetworkRepository struct {
	db *sqlx.DB
}

func NewBlockchainNetworkRepository(db *sqlx.DB) *BlockchainNetworkRepository {
	return &BlockchainNetworkRepository{db: db}
}

func (r *BlockchainNetworkRepository) Create(ctx context.Context, network *domain.BlockchainNetworkInfo) error {
	query := `
		INSERT INTO customer_schema.blockchain_networks (
			network_id, name, status, block_height, peer_count, 
			last_block_time, channel, rpc_url, chain_id, symbol, created_at, updated_at
		) VALUES (
			:network_id, :name, :status, :block_height, :peer_count,
			:last_block_time, :channel, :rpc_url, :chain_id, :symbol, :created_at, :updated_at
		)
	`
	_, err := r.db.NamedExecContext(ctx, query, network)
	if err != nil {
		return errors.Wrap(err, "failed to create blockchain network")
	}
	return nil
}

func (r *BlockchainNetworkRepository) Update(ctx context.Context, network *domain.BlockchainNetworkInfo) error {
	query := `
		UPDATE customer_schema.blockchain_networks SET
			name = :name,
			status = :status,
			block_height = :block_height,
			peer_count = :peer_count,
			last_block_time = :last_block_time,
			channel = :channel,
			rpc_url = :rpc_url,
			chain_id = :chain_id,
			symbol = :symbol,
			updated_at = :updated_at
		WHERE network_id = :network_id
	`
	_, err := r.db.NamedExecContext(ctx, query, network)
	if err != nil {
		return errors.Wrap(err, "failed to update blockchain network")
	}
	return nil
}

func (r *BlockchainNetworkRepository) FindByID(ctx context.Context, id string) (*domain.BlockchainNetworkInfo, error) {
	var network domain.BlockchainNetworkInfo
	query := `SELECT * FROM customer_schema.blockchain_networks WHERE network_id = $1`
	err := r.db.GetContext(ctx, &network, query, id)
	if err == sql.ErrNoRows {
		return nil, errors.New("blockchain network not found")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find blockchain network")
	}
	return &network, nil
}

func (r *BlockchainNetworkRepository) FindAll(ctx context.Context) ([]*domain.BlockchainNetworkInfo, error) {
	var networks []*domain.BlockchainNetworkInfo
	query := `SELECT * FROM customer_schema.blockchain_networks ORDER BY created_at DESC`
	err := r.db.SelectContext(ctx, &networks, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list blockchain networks")
	}
	return networks, nil
}

func (r *BlockchainNetworkRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM customer_schema.blockchain_networks WHERE network_id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "failed to delete blockchain network")
	}
	return nil
}

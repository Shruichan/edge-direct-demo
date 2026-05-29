package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romedrori/edge-direct-demo/internal/device"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	// Bound the pool so a runaway control plane can't drown the DB.
	cfg.MaxConns = 20
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() { p.pool.Close() }

func (p *Postgres) CreateDevice(ctx context.Context, d device.Device) error {
	_, err := p.pool.Exec(ctx, `
		insert into devices (id, tenant_id, serial, status, enrolled_at, last_seen, cert_serial)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (id) do update set
		  serial = excluded.serial,
		  status = excluded.status,
		  cert_serial = excluded.cert_serial
	`, d.ID, d.TenantID, d.Serial, string(d.Status), d.EnrolledAt, nullTime(d.LastSeen), d.CertSerial)
	return err
}

func (p *Postgres) GetDevice(ctx context.Context, id string) (device.Device, error) {
	var d device.Device
	var status string
	var lastSeen *time.Time
	err := p.pool.QueryRow(ctx, `
		select id, tenant_id, serial, status, enrolled_at, last_seen, cert_serial
		from devices where id = $1
	`, id).Scan(&d.ID, &d.TenantID, &d.Serial, &status, &d.EnrolledAt, &lastSeen, &d.CertSerial)
	if errors.Is(err, pgx.ErrNoRows) {
		return device.Device{}, ErrNotFound
	}
	if err != nil {
		return device.Device{}, err
	}
	d.Status = device.Status(status)
	if lastSeen != nil {
		d.LastSeen = *lastSeen
	}
	return d, nil
}

func (p *Postgres) ListDevices(ctx context.Context, tenantID string) ([]device.Device, error) {
	rows, err := p.pool.Query(ctx, `
		select id, tenant_id, serial, status, enrolled_at, last_seen, cert_serial
		from devices
		where ($1 = '' or tenant_id = $1)
		order by enrolled_at asc
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []device.Device
	for rows.Next() {
		var d device.Device
		var status string
		var lastSeen *time.Time
		if err := rows.Scan(&d.ID, &d.TenantID, &d.Serial, &status, &d.EnrolledAt, &lastSeen, &d.CertSerial); err != nil {
			return nil, err
		}
		d.Status = device.Status(status)
		if lastSeen != nil {
			d.LastSeen = *lastSeen
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateStatus(ctx context.Context, id string, status device.Status, certSerial string) error {
	tag, err := p.pool.Exec(ctx, `
		update devices set status = $2, cert_serial = coalesce(nullif($3,''), cert_serial)
		where id = $1
	`, id, string(status), certSerial)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) MarkSeen(ctx context.Context, id string, at time.Time) error {
	// Only move last_seen forward — telemetry can arrive out of order off the bus.
	tag, err := p.pool.Exec(ctx, `
		update devices set last_seen = $2 where id = $1 and (last_seen is null or last_seen < $2)
	`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either the device doesn't exist or we already had a newer sample. The
		// second case is fine; for the first the upstream caller checks.
		return nil
	}
	return nil
}

func (p *Postgres) InsertTelemetry(ctx context.Context, t device.Telemetry) error {
	_, err := p.pool.Exec(ctx, `
		insert into telemetry (device_id, tenant_id, at, uptime, clients, bssids)
		values ($1, $2, $3, $4, $5, $6)
	`, t.DeviceID, t.TenantID, t.At, t.Uptime, t.Clients, t.BSSIDs)
	return err
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

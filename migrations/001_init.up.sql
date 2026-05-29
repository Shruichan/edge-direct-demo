create table if not exists devices (
    id           text primary key,
    tenant_id    text not null,
    serial       text not null,
    status       text not null,
    enrolled_at  timestamptz not null,
    last_seen    timestamptz,
    cert_serial  text not null default ''
);

create index if not exists devices_tenant_idx on devices (tenant_id);
create index if not exists devices_last_seen_idx on devices (last_seen);

create table if not exists telemetry (
    id          bigserial primary key,
    device_id   text not null references devices(id) on delete cascade,
    tenant_id   text not null,
    at          timestamptz not null,
    uptime      bigint not null,
    clients     int not null,
    bssids      text[] not null default '{}'
);

create index if not exists telemetry_device_at_idx on telemetry (device_id, at desc);

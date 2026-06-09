-- 数据库初始化脚本
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "btree_gin";

-- 元数据表：记录所有分表信息
CREATE TABLE IF NOT EXISTS span_tables (
    table_name TEXT PRIMARY KEY,
    date DATE NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 服务元数据表
CREATE TABLE IF NOT EXISTS services (
    id SERIAL PRIMARY KEY,
    service_name TEXT NOT NULL UNIQUE,
    first_seen TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 操作元数据表
CREATE TABLE IF NOT EXISTS operations (
    id SERIAL PRIMARY KEY,
    service_name TEXT NOT NULL,
    operation_name TEXT NOT NULL,
    first_seen TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(service_name, operation_name)
);

-- 服务拓扑边数据表
CREATE TABLE IF NOT EXISTS topology_edges (
    id SERIAL PRIMARY KEY,
    source_service TEXT NOT NULL,
    target_service TEXT NOT NULL,
    call_count BIGINT NOT NULL DEFAULT 0,
    avg_latency DOUBLE PRECISION NOT NULL DEFAULT 0,
    p99_latency DOUBLE PRECISION NOT NULL DEFAULT 0,
    error_count BIGINT NOT NULL DEFAULT 0,
    window_start TIMESTAMP NOT NULL,
    window_end TIMESTAMP NOT NULL,
    window_type TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_service, target_service, window_start, window_type)
);

CREATE INDEX IF NOT EXISTS idx_topology_edges_window ON topology_edges(window_start, window_type);

-- Span表模板（按天分表）
CREATE OR REPLACE FUNCTION create_span_table(date_str TEXT) RETURNS VOID AS $$
DECLARE
    table_name TEXT := 'spans_' || date_str;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_tables WHERE tablename = table_name) THEN
        EXECUTE format('
            CREATE TABLE IF NOT EXISTS %I (
                trace_id TEXT NOT NULL,
                span_id TEXT NOT NULL,
                parent_span_id TEXT,
                service_name TEXT NOT NULL,
                operation_name TEXT NOT NULL,
                start_time TIMESTAMP NOT NULL,
                end_time TIMESTAMP NOT NULL,
                duration_ms BIGINT NOT NULL,
                status_code INTEGER NOT NULL DEFAULT 0,
                attributes JSONB,
                is_orphan BOOLEAN NOT NULL DEFAULT FALSE,
                created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                PRIMARY KEY (trace_id, span_id)
            )
        ', table_name);

        EXECUTE format('CREATE INDEX IF NOT EXISTS %I_trace_id_idx ON %I (trace_id)', table_name || '_trace_id', table_name);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I_time_idx ON %I (start_time, end_time)', table_name || '_time', table_name);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I_service_idx ON %I (service_name, operation_name)', table_name || '_service', table_name);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I_duration_idx ON %I (duration_ms)', table_name || '_duration', table_name);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I_status_idx ON %I (status_code)', table_name || '_status', table_name);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I_attrs_idx ON %I USING GIN (attributes)', table_name || '_attrs', table_name);

        INSERT INTO span_tables (table_name, date)
        VALUES (table_name, to_date(date_str, 'YYYYMMDD'))
        ON CONFLICT DO NOTHING;
    END IF;
END;
$$ LANGUAGE plpgsql;

-- 自动创建今天和未来7天的表
SELECT create_span_table(to_char(d, 'YYYYMMDD'))
FROM generate_series(
    CURRENT_DATE,
    CURRENT_DATE + INTERVAL '7 days',
    INTERVAL '1 day'
) AS d;

-- 创建清理旧表的函数
CREATE OR REPLACE FUNCTION cleanup_old_span_tables(retention_days INTEGER) RETURNS INTEGER AS $$
DECLARE
    drop_count INTEGER := 0;
    rec RECORD;
BEGIN
    FOR rec IN
        SELECT table_name, date
        FROM span_tables
        WHERE date < CURRENT_DATE - retention_days * INTERVAL '1 day'
        ORDER BY date ASC
    LOOP
        EXECUTE format('DROP TABLE IF EXISTS %I', rec.table_name);
        DELETE FROM span_tables WHERE table_name = rec.table_name;
        drop_count := drop_count + 1;
    END LOOP;
    RETURN drop_count;
END;
$$ LANGUAGE plpgsql;

-- Trace摘要表
CREATE TABLE IF NOT EXISTS trace_summaries (
    trace_id TEXT PRIMARY KEY,
    root_service TEXT,
    root_operation TEXT,
    total_duration_ms BIGINT NOT NULL,
    span_count INTEGER NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP NOT NULL,
    status_code INTEGER NOT NULL DEFAULT 0,
    is_slow BOOLEAN NOT NULL DEFAULT FALSE,
    is_anomaly BOOLEAN NOT NULL DEFAULT FALSE,
    is_retry_storm BOOLEAN NOT NULL DEFAULT FALSE,
    critical_path TEXT[],
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_trace_summaries_time ON trace_summaries(start_time);
CREATE INDEX IF NOT EXISTS idx_trace_summaries_service ON trace_summaries(root_service);
CREATE INDEX IF NOT EXISTS idx_trace_summaries_status ON trace_summaries(is_slow, is_anomaly, is_retry_storm);

-- P99阈值缓存表（用于慢请求检测）
CREATE TABLE IF NOT EXISTS operation_p99 (
    service_name TEXT NOT NULL,
    operation_name TEXT NOT NULL,
    p99_latency DOUBLE PRECISION NOT NULL,
    calculated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (service_name, operation_name)
);

-- 采样配置表
CREATE TABLE IF NOT EXISTS sampling_config (
    id SERIAL PRIMARY KEY,
    head_sampling_rate DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    tail_normal_rate DOUBLE PRECISION NOT NULL DEFAULT 0.1,
    tail_anomaly_rate DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO sampling_config (head_sampling_rate, tail_normal_rate, tail_anomaly_rate)
VALUES (1.0, 0.1, 1.0)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS service_health_scores (
    id SERIAL PRIMARY KEY,
    service_name TEXT NOT NULL,
    score INTEGER NOT NULL CHECK (score >= 0 AND score <= 100),
    error_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    error_rate_score INTEGER NOT NULL DEFAULT 100,
    p99_deviation DOUBLE PRECISION NOT NULL DEFAULT 0,
    p99_deviation_score INTEGER NOT NULL DEFAULT 100,
    upstream_success_rate DOUBLE PRECISION NOT NULL DEFAULT 1,
    upstream_success_rate_score INTEGER NOT NULL DEFAULT 100,
    p99_baseline DOUBLE PRECISION NOT NULL DEFAULT 0,
    calculated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(service_name, calculated_at)
);

CREATE INDEX IF NOT EXISTS idx_health_scores_service ON service_health_scores(service_name, calculated_at DESC);

CREATE TABLE IF NOT EXISTS service_health_baselines (
    service_name TEXT PRIMARY KEY,
    p99_baseline DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS alert_rules (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    type TEXT NOT NULL CHECK (type IN ('threshold', 'spike', 'topology')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    severity TEXT NOT NULL DEFAULT 'warning' CHECK (severity IN ('info', 'warning', 'critical')),
    service_name TEXT,
    metric TEXT NOT NULL,
    operator TEXT NOT NULL DEFAULT '>' CHECK (operator IN ('>', '>=', '<', '<=', '==', '!=')),
    threshold DOUBLE PRECISION NOT NULL,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    spike_window_minutes INTEGER NOT NULL DEFAULT 60,
    spike_multiplier DOUBLE PRECISION NOT NULL DEFAULT 2.0,
    topology_check TEXT DEFAULT 'all_downstream_inactive',
    cooldown_seconds INTEGER NOT NULL DEFAULT 300,
    last_triggered_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS alert_events (
    id SERIAL PRIMARY KEY,
    rule_id INTEGER NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    rule_name TEXT NOT NULL,
    severity TEXT NOT NULL,
    service_name TEXT,
    metric_value DOUBLE PRECISION NOT NULL,
    threshold DOUBLE PRECISION NOT NULL,
    message TEXT,
    trace_ids TEXT[],
    fired_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP,
    acknowledged BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_alert_events_rule ON alert_events(rule_id, fired_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_events_fired ON alert_events(fired_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_events_service ON alert_events(service_name, fired_at DESC);

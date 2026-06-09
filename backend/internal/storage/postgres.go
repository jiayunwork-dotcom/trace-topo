package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"trace-topo/internal/config"
	"trace-topo/internal/model"
)

type PostgresStore struct {
	pool        *pgxpool.Pool
	tableCache  map[string]bool
	tableMu     sync.RWMutex
	writeBatch  []*model.Span
	writeMu     sync.Mutex
	writeTicker *time.Ticker
}

func NewPostgresStore(ctx context.Context) (*PostgresStore, error) {
	poolConfig, err := pgxpool.ParseConfig(config.AppConfig.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	poolConfig.MaxConns = 32
	poolConfig.MinConns = 4
	poolConfig.MaxConnLifetime = 1 * time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &PostgresStore{
		pool:       pool,
		tableCache: make(map[string]bool),
		writeBatch: make([]*model.Span, 0, 1000),
	}

	if err := store.ensureFutureTables(ctx); err != nil {
		logrus.Warnf("Failed to pre-create future tables: %v", err)
	}

	go store.startHousekeeping(ctx)
	go store.startBatchWriter(ctx)

	logrus.Info("PostgreSQL store initialized")
	return store, nil
}

func (s *PostgresStore) Close() {
	if s.writeTicker != nil {
		s.writeTicker.Stop()
	}
	s.pool.Close()
}

func (s *PostgresStore) getTableName(t time.Time) string {
	return fmt.Sprintf("spans_%s", t.Format("20060102"))
}

func (s *PostgresStore) ensureTable(ctx context.Context, tableName string, dateStr string) error {
	s.tableMu.RLock()
	exists := s.tableCache[tableName]
	s.tableMu.RUnlock()

	if exists {
		return nil
	}

	_, err := s.pool.Exec(ctx, "SELECT create_span_table($1)", dateStr)
	if err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	s.tableMu.Lock()
	s.tableCache[tableName] = true
	s.tableMu.Unlock()

	logrus.Infof("Created span table: %s", tableName)
	return nil
}

func (s *PostgresStore) ensureFutureTables(ctx context.Context) error {
	for i := 0; i < 7; i++ {
		date := time.Now().AddDate(0, 0, i)
		dateStr := date.Format("20060102")
		tableName := s.getTableName(date)
		if err := s.ensureTable(ctx, tableName, dateStr); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) startHousekeeping(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupOldTables(ctx)
			s.ensureFutureTables(ctx)
		}
	}
}

func (s *PostgresStore) cleanupOldTables(ctx context.Context) {
	var dropCount int
	err := s.pool.QueryRow(ctx, "SELECT cleanup_old_span_tables($1)", config.AppConfig.RetentionDays).Scan(&dropCount)
	if err != nil {
		logrus.Errorf("Failed to cleanup old tables: %v", err)
		return
	}
	if dropCount > 0 {
		logrus.Infof("Cleaned up %d old span tables", dropCount)

		s.tableMu.Lock()
		s.tableCache = make(map[string]bool)
		s.tableMu.Unlock()
	}
}

func (s *PostgresStore) startBatchWriter(ctx context.Context) {
	s.writeTicker = time.NewTicker(1 * time.Second)
	defer s.writeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.flushBatch(ctx)
			return
		case <-s.writeTicker.C:
			s.flushBatch(ctx)
		}
	}
}

func (s *PostgresStore) WriteSpans(ctx context.Context, spans []*model.Span) error {
	if len(spans) == 0 {
		return nil
	}

	s.writeMu.Lock()
	s.writeBatch = append(s.writeBatch, spans...)
	batchLen := len(s.writeBatch)
	s.writeMu.Unlock()

	if batchLen >= 1000 {
		s.flushBatch(ctx)
	}

	return nil
}

func (s *PostgresStore) flushBatch(ctx context.Context) {
	s.writeMu.Lock()
	if len(s.writeBatch) == 0 {
		s.writeMu.Unlock()
		return
	}

	spans := s.writeBatch
	s.writeBatch = make([]*model.Span, 0, 1000)
	s.writeMu.Unlock()

	spansByTable := make(map[string][]*model.Span)
	for _, span := range spans {
		tableName := s.getTableName(span.StartTime)
		spansByTable[tableName] = append(spansByTable[tableName], span)
	}

	for tableName, tableSpans := range spansByTable {
		dateStr := tableSpans[0].StartTime.Format("20060102")
		if err := s.ensureTable(ctx, tableName, dateStr); err != nil {
			logrus.Errorf("Failed to ensure table %s: %v", tableName, err)
			continue
		}

		if err := s.insertSpansBatch(ctx, tableName, tableSpans); err != nil {
			logrus.Errorf("Failed to insert spans into %s: %v", tableName, err)
		}
	}

	if err := s.updateServiceMetadata(ctx, spans); err != nil {
		logrus.Warnf("Failed to update service metadata: %v", err)
	}
}

func (s *PostgresStore) insertSpansBatch(ctx context.Context, tableName string, spans []*model.Span) error {
	if len(spans) == 0 {
		return nil
	}

	rows := make([][]interface{}, 0, len(spans))
	for _, span := range spans {
		attrsJSON, err := json.Marshal(span.Attributes)
		if err != nil {
			attrsJSON = []byte("{}")
		}

		rows = append(rows, []interface{}{
			span.TraceID,
			span.SpanID,
			span.ParentSpanID,
			span.ServiceName,
			span.OperationName,
			span.StartTime,
			span.EndTime,
			span.DurationMs,
			span.StatusCode,
			attrsJSON,
			span.IsOrphan,
		})
	}

	copyCount, err := s.pool.CopyFrom(
		ctx,
		pgx.Identifier{tableName},
		[]string{
			"trace_id", "span_id", "parent_span_id",
			"service_name", "operation_name",
			"start_time", "end_time", "duration_ms",
			"status_code", "attributes", "is_orphan",
		},
		pgx.CopyFromRows(rows),
	)

	if err != nil {
		return fmt.Errorf("COPY failed: %w", err)
	}

	logrus.Debugf("Inserted %d spans into %s", copyCount, tableName)
	return nil
}

func (s *PostgresStore) updateServiceMetadata(ctx context.Context, spans []*model.Span) error {
	serviceOps := make(map[string]map[string]bool)
	for _, span := range spans {
		if _, ok := serviceOps[span.ServiceName]; !ok {
			serviceOps[span.ServiceName] = make(map[string]bool)
		}
		serviceOps[span.ServiceName][span.OperationName] = true
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for service, ops := range serviceOps {
		_, err := tx.Exec(ctx, `
			INSERT INTO services (service_name, last_seen)
			VALUES ($1, NOW())
			ON CONFLICT (service_name) DO UPDATE
			SET last_seen = NOW()
		`, service)
		if err != nil {
			return err
		}

		for op := range ops {
			_, err := tx.Exec(ctx, `
				INSERT INTO operations (service_name, operation_name, last_seen)
				VALUES ($1, $2, NOW())
				ON CONFLICT (service_name, operation_name) DO UPDATE
				SET last_seen = NOW()
			`, service, op)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) WriteTraceSummary(ctx context.Context, trace *model.Trace) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO trace_summaries (
			trace_id, root_service, root_operation,
			total_duration_ms, span_count, start_time, end_time,
			status_code, is_slow, is_anomaly, is_retry_storm, critical_path
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (trace_id) DO UPDATE
		SET root_service = EXCLUDED.root_service,
			root_operation = EXCLUDED.root_operation,
			total_duration_ms = EXCLUDED.total_duration_ms,
			span_count = EXCLUDED.span_count,
			start_time = EXCLUDED.start_time,
			end_time = EXCLUDED.end_time,
			status_code = EXCLUDED.status_code,
			is_slow = EXCLUDED.is_slow,
			is_anomaly = EXCLUDED.is_anomaly,
			is_retry_storm = EXCLUDED.is_retry_storm,
			critical_path = EXCLUDED.critical_path
	`,
		trace.TraceID,
		trace.RootService,
		trace.RootOperation,
		trace.TotalDuration,
		trace.SpanCount,
		trace.StartTime,
		trace.EndTime,
		trace.StatusCode,
		trace.IsSlow,
		trace.IsAnomaly,
		trace.IsRetryStorm,
		trace.CriticalPath,
	)
	return err
}

func (s *PostgresStore) GetTraceSummary(ctx context.Context, traceID string) (*model.TraceSummary, error) {
	var ts model.TraceSummary
	err := s.pool.QueryRow(ctx, `
		SELECT trace_id, root_service, root_operation,
			total_duration_ms, span_count, start_time, end_time,
			status_code, is_slow, is_anomaly, is_retry_storm
		FROM trace_summaries
		WHERE trace_id = $1
	`, traceID).Scan(
		&ts.TraceID, &ts.RootService, &ts.RootOperation,
		&ts.TotalDuration, &ts.SpanCount, &ts.StartTime, &ts.EndTime,
		&ts.StatusCode, &ts.IsSlow, &ts.IsAnomaly, &ts.IsRetryStorm,
	)
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func (s *PostgresStore) GetTraceSpans(ctx context.Context, traceID string, startTime, endTime time.Time) ([]*model.Span, error) {
	tables, err := s.getTablesInRange(ctx, startTime, endTime)
	if err != nil {
		return nil, err
	}

	if len(tables) == 0 {
		return nil, nil
	}

	var queries []string
	var args []interface{}
	argIdx := 1

	for _, table := range tables {
		queries = append(queries, fmt.Sprintf(`
			SELECT trace_id, span_id, parent_span_id, service_name, operation_name,
				start_time, end_time, duration_ms, status_code, attributes, is_orphan
			FROM %s
			WHERE trace_id = $%d
		`, pgx.Identifier{table}.Sanitize(), argIdx))
		args = append(args, traceID)
		argIdx++
	}

	query := strings.Join(queries, " UNION ALL ") + " ORDER BY start_time ASC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var spans []*model.Span
	for rows.Next() {
		var span model.Span
		var attrsJSON []byte
		err := rows.Scan(
			&span.TraceID, &span.SpanID, &span.ParentSpanID,
			&span.ServiceName, &span.OperationName,
			&span.StartTime, &span.EndTime, &span.DurationMs,
			&span.StatusCode, &attrsJSON, &span.IsOrphan,
		)
		if err != nil {
			return nil, err
		}

		if len(attrsJSON) > 0 {
			json.Unmarshal(attrsJSON, &span.Attributes)
		}

		spans = append(spans, &span)
	}

	return spans, rows.Err()
}

func (s *PostgresStore) SearchTraces(ctx context.Context, req *model.SearchRequest) ([]*model.TraceSummary, int64, error) {
	query := `
		SELECT trace_id, root_service, root_operation,
			total_duration_ms, span_count, start_time, end_time,
			status_code, is_slow, is_anomaly, is_retry_storm
		FROM trace_summaries
		WHERE 1=1
	`

	var args []interface{}
	argIdx := 1

	if req.ServiceName != "" {
		query += fmt.Sprintf(" AND root_service = $%d", argIdx)
		args = append(args, req.ServiceName)
		argIdx++
	}

	if req.OperationName != "" {
		query += fmt.Sprintf(" AND root_operation = $%d", argIdx)
		args = append(args, req.OperationName)
		argIdx++
	}

	if !req.StartTime.IsZero() {
		query += fmt.Sprintf(" AND start_time >= $%d", argIdx)
		args = append(args, req.StartTime)
		argIdx++
	}

	if !req.EndTime.IsZero() {
		query += fmt.Sprintf(" AND end_time <= $%d", argIdx)
		args = append(args, req.EndTime)
		argIdx++
	}

	if req.MinDuration > 0 {
		query += fmt.Sprintf(" AND total_duration_ms >= $%d", argIdx)
		args = append(args, req.MinDuration)
		argIdx++
	}

	if req.MaxDuration > 0 {
		query += fmt.Sprintf(" AND total_duration_ms <= $%d", argIdx)
		args = append(args, req.MaxDuration)
		argIdx++
	}

	if req.StatusCode != nil {
		query += fmt.Sprintf(" AND status_code = $%d", argIdx)
		args = append(args, *req.StatusCode)
		argIdx++
	}

	if req.OnlyAnomaly {
		query += " AND (is_slow OR is_anomaly OR is_retry_storm)"
	}

	countQuery := strings.Replace(query, "SELECT trace_id,", "SELECT COUNT(*)", 1)
	var total int64
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query += " ORDER BY start_time DESC"

	if req.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", req.Limit)
	} else {
		query += " LIMIT 100"
	}

	if req.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", req.Offset)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []*model.TraceSummary
	for rows.Next() {
		var ts model.TraceSummary
		err := rows.Scan(
			&ts.TraceID, &ts.RootService, &ts.RootOperation,
			&ts.TotalDuration, &ts.SpanCount, &ts.StartTime, &ts.EndTime,
			&ts.StatusCode, &ts.IsSlow, &ts.IsAnomaly, &ts.IsRetryStorm,
		)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, &ts)
	}

	return results, total, rows.Err()
}

func (s *PostgresStore) GetServices(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT service_name FROM services
		WHERE last_seen > NOW() - INTERVAL '24 hours'
		ORDER BY service_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		services = append(services, s)
	}
	return services, rows.Err()
}

func (s *PostgresStore) GetOperations(ctx context.Context, serviceName string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT operation_name FROM operations
		WHERE service_name = $1 AND last_seen > NOW() - INTERVAL '24 hours'
		ORDER BY operation_name
	`, serviceName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []string
	for rows.Next() {
		var op string
		if err := rows.Scan(&op); err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func (s *PostgresStore) GetAnomalyTraces(ctx context.Context, limit int) ([]*model.TraceSummary, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT trace_id, root_service, root_operation,
			total_duration_ms, span_count, start_time, end_time,
			status_code, is_slow, is_anomaly, is_retry_storm
		FROM trace_summaries
		WHERE is_slow OR is_anomaly OR is_retry_storm
		ORDER BY start_time DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*model.TraceSummary
	for rows.Next() {
		var ts model.TraceSummary
		err := rows.Scan(
			&ts.TraceID, &ts.RootService, &ts.RootOperation,
			&ts.TotalDuration, &ts.SpanCount, &ts.StartTime, &ts.EndTime,
			&ts.StatusCode, &ts.IsSlow, &ts.IsAnomaly, &ts.IsRetryStorm,
		)
		if err != nil {
			return nil, err
		}
		results = append(results, &ts)
	}

	return results, rows.Err()
}

func (s *PostgresStore) getTablesInRange(ctx context.Context, start, end time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT table_name FROM span_tables
		WHERE date >= $1::date AND date <= $2::date
		ORDER BY date
	`, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (s *PostgresStore) UpdateOperationP99(ctx context.Context, serviceName, operationName string, p99 float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO operation_p99 (service_name, operation_name, p99_latency)
		VALUES ($1, $2, $3)
		ON CONFLICT (service_name, operation_name) DO UPDATE
		SET p99_latency = EXCLUDED.p99_latency,
			calculated_at = NOW()
	`, serviceName, operationName, p99)
	return err
}

func (s *PostgresStore) GetOperationP99(ctx context.Context, serviceName, operationName string) (float64, error) {
	var p99 float64
	err := s.pool.QueryRow(ctx, `
		SELECT p99_latency FROM operation_p99
		WHERE service_name = $1 AND operation_name = $2
	`, serviceName, operationName).Scan(&p99)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return p99, nil
}

func (s *PostgresStore) GetAllOperationP99(ctx context.Context) (map[string]float64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT service_name, operation_name, p99_latency
		FROM operation_p99
		WHERE calculated_at > NOW() - INTERVAL '1 hour'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var svc, op string
		var p99 float64
		if err := rows.Scan(&svc, &op, &p99); err != nil {
			return nil, err
		}
		key := svc + ":" + op
		result[key] = p99
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetMetricsTrend(ctx context.Context, duration time.Duration) ([]*model.TrendPoint, error) {
	interval := "5 minutes"
	if duration > 24*time.Hour {
		interval = "1 hour"
	}

	query := fmt.Sprintf(`
		SELECT
			date_trunc('%s', start_time) as bucket,
			COUNT(*) as qps,
			AVG(duration_ms) as avg_latency,
			SUM(CASE WHEN status_code != 0 THEN 1 ELSE 0 END)::float / COUNT(*) as error_rate
		FROM trace_summaries
		WHERE start_time > NOW() - $1::interval
		GROUP BY bucket
		ORDER BY bucket
	`, interval)

	rows, err := s.pool.Query(ctx, query, duration.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []*model.TrendPoint
	for rows.Next() {
		var p model.TrendPoint
		var qps int64
		err := rows.Scan(&p.Timestamp, &qps, &p.AvgLatency, &p.ErrorRate)
		if err != nil {
			return nil, err
		}
		p.QPS = qps / (5 * 60)
		points = append(points, &p)
	}

	return points, rows.Err()
}

func (s *PostgresStore) GetRealtimeMetrics(ctx context.Context) (*model.Metrics, error) {
	var m model.Metrics
	m.Timestamp = time.Now()

	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) / 60.0 as qps,
			COALESCE(AVG(total_duration_ms), 0) as avg_latency,
			COALESCE(SUM(CASE WHEN status_code != 0 THEN 1 ELSE 0 END)::float / COUNT(*), 0) as error_rate
		FROM trace_summaries
		WHERE start_time > NOW() - INTERVAL '1 minute'
	`).Scan(&m.TotalQPS, &m.AvgLatency, &m.ErrorRate)
	if err != nil {
		return nil, err
	}

	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM services
		WHERE last_seen > NOW() - INTERVAL '5 minutes'
	`).Scan(&m.ActiveServices)
	if err != nil {
		return nil, err
	}

	return &m, nil
}

func (s *PostgresStore) GetSamplingConfig(ctx context.Context) (*model.SamplingConfig, error) {
	var cfg model.SamplingConfig
	err := s.pool.QueryRow(ctx, `
		SELECT head_sampling_rate, tail_normal_rate, tail_anomaly_rate
		FROM sampling_config
		ORDER BY id DESC LIMIT 1
	`).Scan(&cfg.HeadRate, &cfg.TailNormalRate, &cfg.TailAnomalyRate)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *PostgresStore) UpdateSamplingConfig(ctx context.Context, headRate, tailNormalRate, tailAnomalyRate float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sampling_config (head_sampling_rate, tail_normal_rate, tail_anomaly_rate, updated_at)
		VALUES ($1, $2, $3, NOW())
	`, headRate, tailNormalRate, tailAnomalyRate)
	return err
}

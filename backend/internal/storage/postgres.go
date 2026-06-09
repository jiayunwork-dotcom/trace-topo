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

func (s *PostgresStore) GetServiceErrorRate(ctx context.Context, serviceName string, window time.Duration) (float64, error) {
	var errorRate float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(
			SUM(CASE WHEN status_code != 0 THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0),
			0
		) as error_rate
		FROM trace_summaries
		WHERE root_service = $1 AND start_time > NOW() - $2::interval
	`, serviceName, window.String()).Scan(&errorRate)
	if err != nil {
		return 0, err
	}
	return errorRate, nil
}

func (s *PostgresStore) GetServiceP99Latency(ctx context.Context, serviceName string, window time.Duration) (float64, error) {
	var p99 float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY total_duration_ms), 0)
		FROM trace_summaries
		WHERE root_service = $1 AND start_time > NOW() - $2::interval
	`, serviceName, window.String()).Scan(&p99)
	if err != nil {
		return 0, err
	}
	return p99, nil
}

func (s *PostgresStore) GetServiceUpstreamSuccessRate(ctx context.Context, serviceName string, window time.Duration) (float64, error) {
	var successRate float64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(
			SUM(CASE WHEN status_code = 0 THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0),
			1.0
		)
		FROM trace_summaries
		WHERE root_service = $1 AND start_time > NOW() - $2::interval
	`, serviceName, window.String()).Scan(&successRate)
	if err != nil {
		return 1.0, err
	}
	return successRate, nil
}

func (s *PostgresStore) GetServiceAvgMetric(ctx context.Context, serviceName, metric string, window time.Duration) (float64, error) {
	var value float64
	var err error

	switch metric {
	case "error_rate":
		value, err = s.GetServiceErrorRate(ctx, serviceName, window)
	case "p99_latency":
		value, err = s.GetServiceP99Latency(ctx, serviceName, window)
	case "avg_latency":
		err = s.pool.QueryRow(ctx, `
			SELECT COALESCE(AVG(total_duration_ms), 0)
			FROM trace_summaries
			WHERE root_service = $1 AND start_time > NOW() - $2::interval
		`, serviceName, window.String()).Scan(&value)
	default:
		err = s.pool.QueryRow(ctx, `
			SELECT COALESCE(AVG(total_duration_ms), 0)
			FROM trace_summaries
			WHERE root_service = $1 AND start_time > NOW() - $2::interval
		`, serviceName, window.String()).Scan(&value)
	}

	return value, err
}

func (s *PostgresStore) GetRecentTraceIDs(ctx context.Context, serviceName string, window time.Duration) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT trace_id FROM trace_summaries
		WHERE root_service = $1 AND start_time > NOW() - $2::interval
		ORDER BY start_time DESC
		LIMIT 10
	`, serviceName, window.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *PostgresStore) GetHealthBaseline(ctx context.Context, serviceName string) (float64, error) {
	var baseline float64
	err := s.pool.QueryRow(ctx, `
		SELECT p99_baseline FROM service_health_baselines
		WHERE service_name = $1
	`, serviceName).Scan(&baseline)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return baseline, nil
}

func (s *PostgresStore) UpsertHealthBaseline(ctx context.Context, serviceName string, baseline float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO service_health_baselines (service_name, p99_baseline, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (service_name) DO UPDATE
		SET p99_baseline = EXCLUDED.p99_baseline, updated_at = NOW()
	`, serviceName, baseline)
	return err
}

func (s *PostgresStore) WriteHealthScore(ctx context.Context, score *model.HealthScore) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO service_health_scores (
			service_name, score, error_rate, error_rate_score,
			p99_deviation, p99_deviation_score,
			upstream_success_rate, upstream_success_rate_score,
			p99_baseline, calculated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (service_name, calculated_at) DO NOTHING
	`, score.ServiceName, score.Score,
		score.ErrorRate, score.ErrorRateScore,
		score.P99Deviation, score.P99DeviationScore,
		score.UpstreamSuccessRate, score.UpstreamSuccessScore,
		score.P99Baseline, score.CalculatedAt)
	return err
}

func (s *PostgresStore) GetHealthScores(ctx context.Context, serviceName string, limit int) ([]*model.HealthScore, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT service_name, score, error_rate, error_rate_score,
			p99_deviation, p99_deviation_score,
			upstream_success_rate, upstream_success_rate_score,
			p99_baseline, calculated_at
		FROM service_health_scores
		WHERE service_name = $1
		ORDER BY calculated_at DESC
		LIMIT $2
	`, serviceName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scores []*model.HealthScore
	for rows.Next() {
		var hs model.HealthScore
		err := rows.Scan(
			&hs.ServiceName, &hs.Score, &hs.ErrorRate, &hs.ErrorRateScore,
			&hs.P99Deviation, &hs.P99DeviationScore,
			&hs.UpstreamSuccessRate, &hs.UpstreamSuccessScore,
			&hs.P99Baseline, &hs.CalculatedAt,
		)
		if err != nil {
			return nil, err
		}
		scores = append(scores, &hs)
	}
	return scores, rows.Err()
}

func (s *PostgresStore) GetAllLatestHealthScores(ctx context.Context) (map[string]*model.HealthScore, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (service_name)
			service_name, score, error_rate, error_rate_score,
			p99_deviation, p99_deviation_score,
			upstream_success_rate, upstream_success_rate_score,
			p99_baseline, calculated_at
		FROM service_health_scores
		ORDER BY service_name, calculated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*model.HealthScore)
	for rows.Next() {
		var hs model.HealthScore
		err := rows.Scan(
			&hs.ServiceName, &hs.Score, &hs.ErrorRate, &hs.ErrorRateScore,
			&hs.P99Deviation, &hs.P99DeviationScore,
			&hs.UpstreamSuccessRate, &hs.UpstreamSuccessScore,
			&hs.P99Baseline, &hs.CalculatedAt,
		)
		if err != nil {
			return nil, err
		}
		result[hs.ServiceName] = &hs
	}
	return result, rows.Err()
}

func (s *PostgresStore) CleanupOldHealthScores(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM service_health_scores
		WHERE calculated_at < NOW() - INTERVAL '7 days'
	`)
	return err
}

func (s *PostgresStore) GetAlertRules(ctx context.Context) ([]*model.AlertRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, type, enabled, severity,
			service_name, metric, operator, threshold,
			duration_seconds, spike_window_minutes, spike_multiplier,
			topology_check, cooldown_seconds, last_triggered_at,
			created_at, updated_at
		FROM alert_rules
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*model.AlertRule
	for rows.Next() {
		var r model.AlertRule
		err := rows.Scan(
			&r.ID, &r.Name, &r.Description, &r.Type, &r.Enabled, &r.Severity,
			&r.ServiceName, &r.Metric, &r.Operator, &r.Threshold,
			&r.DurationSeconds, &r.SpikeWindowMin, &r.SpikeMultiplier,
			&r.TopologyCheck, &r.CooldownSeconds, &r.LastTriggeredAt,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		rules = append(rules, &r)
	}
	return rules, rows.Err()
}

func (s *PostgresStore) GetAlertRule(ctx context.Context, id int) (*model.AlertRule, error) {
	var r model.AlertRule
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, type, enabled, severity,
			service_name, metric, operator, threshold,
			duration_seconds, spike_window_minutes, spike_multiplier,
			topology_check, cooldown_seconds, last_triggered_at,
			created_at, updated_at
		FROM alert_rules
		WHERE id = $1
	`, id).Scan(
		&r.ID, &r.Name, &r.Description, &r.Type, &r.Enabled, &r.Severity,
		&r.ServiceName, &r.Metric, &r.Operator, &r.Threshold,
		&r.DurationSeconds, &r.SpikeWindowMin, &r.SpikeMultiplier,
		&r.TopologyCheck, &r.CooldownSeconds, &r.LastTriggeredAt,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *PostgresStore) CreateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO alert_rules (
			name, description, type, enabled, severity,
			service_name, metric, operator, threshold,
			duration_seconds, spike_window_minutes, spike_multiplier,
			topology_check, cooldown_seconds
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, created_at, updated_at
	`,
		rule.Name, rule.Description, rule.Type, rule.Enabled, rule.Severity,
		rule.ServiceName, rule.Metric, rule.Operator, rule.Threshold,
		rule.DurationSeconds, rule.SpikeWindowMin, rule.SpikeMultiplier,
		rule.TopologyCheck, rule.CooldownSeconds,
	).Scan(&rule.ID, &rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *PostgresStore) UpdateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error) {
	err := s.pool.QueryRow(ctx, `
		UPDATE alert_rules SET
			name = $2, description = $3, type = $4, enabled = $5, severity = $6,
			service_name = $7, metric = $8, operator = $9, threshold = $10,
			duration_seconds = $11, spike_window_minutes = $12, spike_multiplier = $13,
			topology_check = $14, cooldown_seconds = $15, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`,
		rule.ID,
		rule.Name, rule.Description, rule.Type, rule.Enabled, rule.Severity,
		rule.ServiceName, rule.Metric, rule.Operator, rule.Threshold,
		rule.DurationSeconds, rule.SpikeWindowMin, rule.SpikeMultiplier,
		rule.TopologyCheck, rule.CooldownSeconds,
	).Scan(&rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (s *PostgresStore) DeleteAlertRule(ctx context.Context, id int) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) CreateAlertEvent(ctx context.Context, event *model.AlertEvent) (*model.AlertEvent, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO alert_events (
			rule_id, rule_name, severity, service_name,
			metric_value, threshold, message, trace_ids, fired_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`,
		event.RuleID, event.RuleName, event.Severity, event.ServiceName,
		event.MetricValue, event.Threshold, event.Message, event.TraceIDs, event.FiredAt,
	).Scan(&event.ID)
	if err != nil {
		return nil, err
	}
	return event, nil
}

func (s *PostgresStore) GetAlertEvents(ctx context.Context, ruleID *int, serviceName string, limit, offset int) ([]*model.AlertEvent, int64, error) {
	var args []interface{}
	argIdx := 1

	where := "WHERE 1=1"
	if ruleID != nil {
		where += fmt.Sprintf(" AND rule_id = $%d", argIdx)
		args = append(args, *ruleID)
		argIdx++
	}
	if serviceName != "" {
		where += fmt.Sprintf(" AND service_name = $%d", argIdx)
		args = append(args, serviceName)
		argIdx++
	}

	var total int64
	countQuery := "SELECT COUNT(*) FROM alert_events " + where
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(`
		SELECT id, rule_id, rule_name, severity, service_name,
			metric_value, threshold, message, trace_ids,
			fired_at, resolved_at, acknowledged
		FROM alert_events %s
		ORDER BY fired_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var events []*model.AlertEvent
	for rows.Next() {
		var ev model.AlertEvent
		err := rows.Scan(
			&ev.ID, &ev.RuleID, &ev.RuleName, &ev.Severity, &ev.ServiceName,
			&ev.MetricValue, &ev.Threshold, &ev.Message, &ev.TraceIDs,
			&ev.FiredAt, &ev.ResolvedAt, &ev.Acknowledged,
		)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, &ev)
	}

	return events, total, rows.Err()
}

func (s *PostgresStore) GetAlertEvent(ctx context.Context, id int) (*model.AlertEvent, error) {
	var ev model.AlertEvent
	err := s.pool.QueryRow(ctx, `
		SELECT id, rule_id, rule_name, severity, service_name,
			metric_value, threshold, message, trace_ids,
			fired_at, resolved_at, acknowledged
		FROM alert_events
		WHERE id = $1
	`, id).Scan(
		&ev.ID, &ev.RuleID, &ev.RuleName, &ev.Severity, &ev.ServiceName,
		&ev.MetricValue, &ev.Threshold, &ev.Message, &ev.TraceIDs,
		&ev.FiredAt, &ev.ResolvedAt, &ev.Acknowledged,
	)
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

func (s *PostgresStore) AcknowledgeAlertEvent(ctx context.Context, id int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE alert_events SET acknowledged = TRUE WHERE id = $1
	`, id)
	return err
}

func (s *PostgresStore) UpdateRuleLastTriggered(ctx context.Context, ruleID int, triggeredAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE alert_rules SET last_triggered_at = $2 WHERE id = $1
	`, ruleID, triggeredAt)
	return err
}

func (s *PostgresStore) GetSLODefinitions(ctx context.Context) ([]*model.SLODefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, service_name, target_type, target_value, window_type,
			budget_total, budget_unit, latency_threshold_ms, target_qps,
			burn_rate_rules, alert_rule_id, enabled, created_at, updated_at
		FROM slo_definitions
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []*model.SLODefinition
	for rows.Next() {
		var d model.SLODefinition
		var burnRateJSON []byte
		err := rows.Scan(
			&d.ID, &d.Name, &d.ServiceName, &d.TargetType, &d.TargetValue, &d.WindowType,
			&d.BudgetTotal, &d.BudgetUnit, &d.LatencyThresholdMs, &d.TargetQPS,
			&burnRateJSON, &d.AlertRuleID, &d.Enabled, &d.CreatedAt, &d.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if len(burnRateJSON) > 0 {
			json.Unmarshal(burnRateJSON, &d.BurnRateRules)
		}
		defs = append(defs, &d)
	}
	return defs, rows.Err()
}

func (s *PostgresStore) GetSLODefinition(ctx context.Context, id int) (*model.SLODefinition, error) {
	var d model.SLODefinition
	var burnRateJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, service_name, target_type, target_value, window_type,
			budget_total, budget_unit, latency_threshold_ms, target_qps,
			burn_rate_rules, alert_rule_id, enabled, created_at, updated_at
		FROM slo_definitions
		WHERE id = $1
	`, id).Scan(
		&d.ID, &d.Name, &d.ServiceName, &d.TargetType, &d.TargetValue, &d.WindowType,
		&d.BudgetTotal, &d.BudgetUnit, &d.LatencyThresholdMs, &d.TargetQPS,
		&burnRateJSON, &d.AlertRuleID, &d.Enabled, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(burnRateJSON) > 0 {
		json.Unmarshal(burnRateJSON, &d.BurnRateRules)
	}
	return &d, nil
}

func (s *PostgresStore) CreateSLODefinition(ctx context.Context, def *model.SLODefinition) (*model.SLODefinition, error) {
	burnRateJSON, _ := json.Marshal(def.BurnRateRules)
	err := s.pool.QueryRow(ctx, `
		INSERT INTO slo_definitions (
			name, service_name, target_type, target_value, window_type,
			budget_total, budget_unit, latency_threshold_ms, target_qps,
			burn_rate_rules, enabled
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at
	`,
		def.Name, def.ServiceName, def.TargetType, def.TargetValue, def.WindowType,
		def.BudgetTotal, def.BudgetUnit, def.LatencyThresholdMs, def.TargetQPS,
		burnRateJSON, def.Enabled,
	).Scan(&def.ID, &def.CreatedAt, &def.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return def, nil
}

func (s *PostgresStore) UpdateSLODefinition(ctx context.Context, def *model.SLODefinition) (*model.SLODefinition, error) {
	burnRateJSON, _ := json.Marshal(def.BurnRateRules)
	err := s.pool.QueryRow(ctx, `
		UPDATE slo_definitions SET
			name = $2, service_name = $3, target_type = $4, target_value = $5,
			window_type = $6, budget_total = $7, budget_unit = $8,
			latency_threshold_ms = $9, target_qps = $10,
			burn_rate_rules = $11, alert_rule_id = $12, enabled = $13, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`,
		def.ID,
		def.Name, def.ServiceName, def.TargetType, def.TargetValue, def.WindowType,
		def.BudgetTotal, def.BudgetUnit, def.LatencyThresholdMs, def.TargetQPS,
		burnRateJSON, def.AlertRuleID, def.Enabled,
	).Scan(&def.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return def, nil
}

func (s *PostgresStore) DeleteSLODefinition(ctx context.Context, id int) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM slo_definitions WHERE id = $1`, id)
	return err
}

func (s *PostgresStore) WriteSLOBudgetSnapshot(ctx context.Context, snap *model.SLOBudgetSnapshot) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO slo_budget_snapshots (
			slo_id, window_start, window_end, total_events, bad_events,
			error_budget_consumed, error_budget_remaining_pct,
			current_measurement, grain, calculated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (slo_id, grain, calculated_at) DO NOTHING
	`, snap.SLOID, snap.WindowStart, snap.WindowEnd, snap.TotalEvents, snap.BadEvents,
		snap.ErrorBudgetConsumed, snap.ErrorBudgetRemainingPct,
		snap.CurrentMeasurement, snap.Grain, snap.CalculatedAt)
	return err
}

func (s *PostgresStore) GetSLOBudgetSnapshots(ctx context.Context, sloID int, grain string, limit int) ([]*model.SLOBudgetSnapshot, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, slo_id, window_start, window_end, total_events, bad_events,
			error_budget_consumed, error_budget_remaining_pct,
			current_measurement, grain, calculated_at
		FROM slo_budget_snapshots
		WHERE slo_id = $1 AND grain = $2
		ORDER BY calculated_at DESC
		LIMIT $3
	`, sloID, grain, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []*model.SLOBudgetSnapshot
	for rows.Next() {
		var snap model.SLOBudgetSnapshot
		err := rows.Scan(
			&snap.ID, &snap.SLOID, &snap.WindowStart, &snap.WindowEnd,
			&snap.TotalEvents, &snap.BadEvents,
			&snap.ErrorBudgetConsumed, &snap.ErrorBudgetRemainingPct,
			&snap.CurrentMeasurement, &snap.Grain, &snap.CalculatedAt,
		)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, &snap)
	}
	return snaps, rows.Err()
}

func (s *PostgresStore) GetLatestSLOBudgetSnapshot(ctx context.Context, sloID int) (*model.SLOBudgetSnapshot, error) {
	var snap model.SLOBudgetSnapshot
	err := s.pool.QueryRow(ctx, `
		SELECT id, slo_id, window_start, window_end, total_events, bad_events,
			error_budget_consumed, error_budget_remaining_pct,
			current_measurement, grain, calculated_at
		FROM slo_budget_snapshots
		WHERE slo_id = $1
		ORDER BY calculated_at DESC
		LIMIT 1
	`, sloID).Scan(
		&snap.ID, &snap.SLOID, &snap.WindowStart, &snap.WindowEnd,
		&snap.TotalEvents, &snap.BadEvents,
		&snap.ErrorBudgetConsumed, &snap.ErrorBudgetRemainingPct,
		&snap.CurrentMeasurement, &snap.Grain, &snap.CalculatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

func (s *PostgresStore) GetSLOSpanCounts(ctx context.Context, serviceName string, windowStart, windowEnd time.Time) (total int64, errors int64, err error) {
	tables, err := s.getTablesInRange(ctx, windowStart, windowEnd)
	if err != nil {
		return 0, 0, err
	}
	if len(tables) == 0 {
		return 0, 0, nil
	}

	var queries []string
	var args []interface{}
	argIdx := 1

	for _, table := range tables {
		queries = append(queries, fmt.Sprintf(`
			SELECT COUNT(*) as total,
				SUM(CASE WHEN status_code != 0 THEN 1 ELSE 0 END)::bigint as errors
			FROM %s
			WHERE service_name = $%d AND start_time >= $%d AND start_time < $%d
		`, pgx.Identifier{table}.Sanitize(), argIdx, argIdx+1, argIdx+2))
		args = append(args, serviceName, windowStart, windowEnd)
		argIdx += 3
	}

	query := strings.Join(queries, " UNION ALL ")
	subRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return 0, 0, err
	}
	defer subRows.Close()

	for subRows.Next() {
		var t, e int64
		if err := subRows.Scan(&t, &e); err != nil {
			return 0, 0, err
		}
		total += t
		errors += e
	}
	return total, errors, subRows.Err()
}

func (s *PostgresStore) GetSLOSlowSpanCounts(ctx context.Context, serviceName string, thresholdMs float64, windowStart, windowEnd time.Time) (total int64, slow int64, err error) {
	tables, err := s.getTablesInRange(ctx, windowStart, windowEnd)
	if err != nil {
		return 0, 0, err
	}
	if len(tables) == 0 {
		return 0, 0, nil
	}

	var queries []string
	var args []interface{}
	argIdx := 1

	for _, table := range tables {
		queries = append(queries, fmt.Sprintf(`
			SELECT COUNT(*) as total,
				SUM(CASE WHEN duration_ms > $%d THEN 1 ELSE 0 END)::bigint as slow
			FROM %s
			WHERE service_name = $%d AND start_time >= $%d AND start_time < $%d
		`, argIdx, pgx.Identifier{table}.Sanitize(), argIdx+1, argIdx+2, argIdx+3))
		args = append(args, int64(thresholdMs), serviceName, windowStart, windowEnd)
		argIdx += 4
	}

	query := strings.Join(queries, " UNION ALL ")
	subRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return 0, 0, err
	}
	defer subRows.Close()

	for subRows.Next() {
		var t, sl int64
		if err := subRows.Scan(&t, &sl); err != nil {
			return 0, 0, err
		}
		total += t
		slow += sl
	}
	return total, slow, subRows.Err()
}

func (s *PostgresStore) GetSLOServiceQPS(ctx context.Context, serviceName string, windowStart, windowEnd time.Time) (float64, error) {
	tables, err := s.getTablesInRange(ctx, windowStart, windowEnd)
	if err != nil {
		return 0, err
	}
	if len(tables) == 0 {
		return 0, nil
	}

	var queries []string
	var args []interface{}
	argIdx := 1

	for _, table := range tables {
		queries = append(queries, fmt.Sprintf(`
			SELECT COUNT(*) FROM %s
			WHERE service_name = $%d AND start_time >= $%d AND start_time < $%d
		`, pgx.Identifier{table}.Sanitize(), argIdx, argIdx+1, argIdx+2))
		args = append(args, serviceName, windowStart, windowEnd)
		argIdx += 3
	}

	query := strings.Join(queries, " UNION ALL ")
	subRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer subRows.Close()

	var total int64
	for subRows.Next() {
		var c int64
		if err := subRows.Scan(&c); err != nil {
			return 0, err
		}
		total += c
	}

	durationSecs := windowEnd.Sub(windowStart).Seconds()
	if durationSecs <= 0 {
		return 0, nil
	}
	return float64(total) / durationSecs, nil
}

func (s *PostgresStore) GetSLOBurnRateAlerts(ctx context.Context, sloID int, limit int) ([]*model.SLOBurnRateAlert, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, slo_id, window_minutes, burn_rate, threshold, severity,
			alert_event_id, fired_at, resolved_at
		FROM slo_burn_rate_alerts
		WHERE slo_id = $1
		ORDER BY fired_at DESC
		LIMIT $2
	`, sloID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []*model.SLOBurnRateAlert
	for rows.Next() {
		var a model.SLOBurnRateAlert
		err := rows.Scan(
			&a.ID, &a.SLOID, &a.WindowMinutes, &a.BurnRate, &a.Threshold,
			&a.Severity, &a.AlertEventID, &a.FiredAt, &a.ResolvedAt,
		)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, &a)
	}
	return alerts, rows.Err()
}

func (s *PostgresStore) CreateSLOBurnRateAlert(ctx context.Context, a *model.SLOBurnRateAlert) (*model.SLOBurnRateAlert, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO slo_burn_rate_alerts (
			slo_id, window_minutes, burn_rate, threshold, severity,
			alert_event_id, fired_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, a.SLOID, a.WindowMinutes, a.BurnRate, a.Threshold, a.Severity,
		a.AlertEventID, a.FiredAt,
	).Scan(&a.ID)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *PostgresStore) ResolveSLOBurnRateAlerts(ctx context.Context, sloID int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE slo_burn_rate_alerts
		SET resolved_at = NOW()
		WHERE slo_id = $1 AND resolved_at IS NULL
	`, sloID)
	return err
}

func (s *PostgresStore) UpdateSLOAlertRuleID(ctx context.Context, sloID int, alertRuleID int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE slo_definitions SET alert_rule_id = $2, updated_at = NOW()
		WHERE id = $1
	`, sloID, alertRuleID)
	return err
}

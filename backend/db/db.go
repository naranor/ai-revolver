// Package db handles database interactions and statistics tracking
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// RequestStats represents statistics for a single request
type RequestStats struct {
	Timestamp time.Time `json:"timestamp"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	ID        int       `json:"id"`
	Status    int       `json:"status"`
	Latency   int       `json:"latency_ms"`
}

// RateLimitEvent represents a rate limiting incident
type RateLimitEvent struct {
	Timestamp time.Time `json:"timestamp"`
	ResetTime time.Time `json:"reset_time"`
	Provider  string    `json:"provider"`
	ID        int       `json:"id"`
}

// ErrorEvent represents an application or API error
type ErrorEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Provider  string    `json:"provider"`
	ErrorType string    `json:"error_type"`
	Message   string    `json:"message"`
	ID        int       `json:"id"`
}

// DB job types
type jobType int

const (
	jobRequest jobType = iota
	jobRateLimit
	jobError
)

type dbJob struct {
	resetT   time.Time
	provider string
	model    string
	errType  string
	message  string
	typ      jobType
	status   int
	latency  int
	isWarmup bool
}

var (
	// DB is the global database connection
	DB          *sql.DB
	dbChan      chan dbJob
	wg          sync.WaitGroup
	initialized bool
)

// InitDB initializes the database connection and creates tables
func InitDB(path string) error {
	var err error
	DB, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}

	// Optimize SQLite for write performance
	DB.SetMaxOpenConns(1) // SQLite doesn't handle concurrent writes well
	if _, err := DB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}
	if _, err := DB.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fmt.Errorf("failed to set synchronous mode: %w", err)
	}
	if _, err := DB.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return fmt.Errorf("failed to set busy timeout: %w", err)
	}

	if err := createTables(); err != nil {
		return err
	}

	// Migration: add is_warmup column to requests if it doesn't exist
	var columnExists bool
	_ = DB.QueryRow("SELECT COUNT(*) > 0 FROM pragma_table_info('requests') WHERE name='is_warmup'").Scan(&columnExists)
	if !columnExists {
		if _, err := DB.Exec("ALTER TABLE requests ADD COLUMN is_warmup BOOLEAN DEFAULT FALSE"); err != nil {
			return fmt.Errorf("failed to migrate is_warmup column: %w", err)
		}
	}

	// Initialize async job queue
	dbChan = make(chan dbJob, 500)
	initialized = true

	// Start worker pool (2 workers for SQLite)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go worker()
	}

	return nil
}

//nolint:gocyclo // worker loop is naturally complex due to batching logic
func worker() {
	defer wg.Done()
	// Batch buffers
	var requestJobs []dbJob
	var rateLimitJobs []dbJob
	var errorJobs []dbJob

	const maxBatchSize = 100
	const flushInterval = 10 * time.Millisecond
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	clearBuffers := func() {
		requestJobs = nil
		rateLimitJobs = nil
		errorJobs = nil
	}

	for {
		select {
		case job, ok := <-dbChan:
			if !ok {
				if err := flushBatch(requestJobs, rateLimitJobs, errorJobs); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "Final DB batch write failed: %v\n", err)
				}
				return
			}
			switch job.typ {
			case jobRequest:
				requestJobs = append(requestJobs, job)
			case jobRateLimit:
				rateLimitJobs = append(rateLimitJobs, job)
			case jobError:
				errorJobs = append(errorJobs, job)
			}

			if len(requestJobs) >= maxBatchSize || len(rateLimitJobs) >= maxBatchSize || len(errorJobs) >= maxBatchSize {
				if err := flushBatch(requestJobs, rateLimitJobs, errorJobs); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "DB batch write failed: %v\n", err)
				}
				clearBuffers()
			}
		case <-ticker.C:
			if len(requestJobs) > 0 || len(rateLimitJobs) > 0 || len(errorJobs) > 0 {
				if err := flushBatch(requestJobs, rateLimitJobs, errorJobs); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "DB batch write failed: %v\n", err)
				}
				clearBuffers()
			}
		}
	}
}

// flushBatch writes buffered jobs to the database in a transaction
func flushBatch(requests []dbJob, rateLimits []dbJob, errors []dbJob) error {
	if len(requests) == 0 && len(rateLimits) == 0 && len(errors) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Insert requests
	for _, job := range requests {
		if _, err := tx.Exec(
			"INSERT INTO requests(timestamp, provider, model, status, latency_ms, is_warmup) VALUES(?, ?, ?, ?, ?, ?)",
			time.Now(), job.provider, job.model, job.status, job.latency, job.isWarmup,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	// Insert rate limits
	for _, job := range rateLimits {
		if _, err := tx.Exec(
			"INSERT INTO rate_limits(timestamp, provider, reset_time) VALUES(?, ?, ?)",
			time.Now(), job.provider, job.resetT,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	// Insert errors
	for _, job := range errors {
		if _, err := tx.Exec(
			"INSERT INTO errors(timestamp, provider, error_type, message) VALUES(?, ?, ?, ?)",
			time.Now(), job.provider, job.errType, job.message,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func processJob(job dbJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	switch job.typ {
	case jobRequest:
		_, err = DB.ExecContext(ctx,
			"INSERT INTO requests(timestamp, provider, model, status, latency_ms, is_warmup) VALUES(?, ?, ?, ?, ?, ?)",
			time.Now(), job.provider, job.model, job.status, job.latency, job.isWarmup,
		)
	case jobRateLimit:
		_, err = DB.ExecContext(ctx,
			"INSERT INTO rate_limits(timestamp, provider, reset_time) VALUES(?, ?, ?)",
			time.Now(), job.provider, job.resetT,
		)
	case jobError:
		_, err = DB.ExecContext(ctx,
			"INSERT INTO errors(timestamp, provider, error_type, message) VALUES(?, ?, ?, ?)",
			time.Now(), job.provider, job.errType, job.message,
		)
	}
	if err != nil {
		// Log to stderr directly to avoid circular dependency
		fmt.Fprintf(os.Stderr, "DB write failed: %v\n", err)
	}
}

func enqueue(job dbJob) {
	if !initialized {
		return
	}
	select {
	case dbChan <- job:
	default:
		// Channel full, drop to prevent blocking
	}
}

func createTables() error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			status INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			is_warmup BOOLEAN DEFAULT FALSE
		)`,
		`CREATE TABLE IF NOT EXISTS rate_limits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			provider TEXT NOT NULL,
			reset_time DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS errors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			provider TEXT NOT NULL,
			error_type TEXT NOT NULL,
			message TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS stats_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL,
			total_requests INTEGER NOT NULL,
			successful_requests INTEGER NOT NULL,
			failed_requests INTEGER NOT NULL,
			rate_limit_events INTEGER NOT NULL,
			error_events INTEGER NOT NULL,
			avg_latency REAL NOT NULL DEFAULT 0
		)`,
	}

	for _, table := range tables {
		if _, err := DB.Exec(table); err != nil {
			return err
		}
	}

	if _, err := DB.Exec("CREATE INDEX IF NOT EXISTS idx_stats_history_timestamp ON stats_history(timestamp)"); err != nil {
		return err
	}

	// Migration: add avg_latency column if it doesn't exist
	_, _ = DB.Exec("ALTER TABLE stats_history ADD COLUMN avg_latency REAL NOT NULL DEFAULT 0")

	return nil
}

// LogRequest enqueues a request statistics record
func LogRequest(provider, model string, status, latency int, isWarmup bool) {
	enqueue(dbJob{typ: jobRequest, provider: provider, model: model, status: status, latency: latency, isWarmup: isWarmup})
}

// LogRateLimit enqueues a rate limit incident record
func LogRateLimit(provider string, resetTime time.Time) {
	enqueue(dbJob{typ: jobRateLimit, provider: provider, resetT: resetTime})
}

// LogError enqueues an error event record
func LogError(provider, errorType, message string) {
	enqueue(dbJob{typ: jobError, provider: provider, errType: errorType, message: message})
}

// GetStats returns current application statistics
func GetStats() (map[string]interface{}, error) {
	return GetStatsWithPeriod("")
}

// periodToWhereClause converts a period string to a SQL WHERE clause
func periodToWhereClause(period string) string {
	switch period {
	case "hour":
		return " WHERE timestamp >= datetime('now', '-1 hour')"
	case "day":
		return " WHERE timestamp >= datetime('now', '-1 day')"
	case "week":
		return " WHERE timestamp >= datetime('now', '-7 days')"
	case "month":
		return " WHERE timestamp >= datetime('now', '-30 days')"
	default:
		return ""
	}
}

// GetStatsWithPeriod returns application statistics for a specific time period
var GetStatsWithPeriod = func(period string) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	reqWhere := periodToWhereClause(period)
	rlWhere := periodToWhereClause(period)

	// Total requests
	var total int
	if err := DB.QueryRow("SELECT COUNT(*) FROM requests" + reqWhere).Scan(&total); err != nil {
		return nil, err
	}
	stats["total_requests"] = total

	// Successful requests
	var successful int
	successWhere := buildWhereClause(reqWhere, "status < 400")
	if err := DB.QueryRow("SELECT COUNT(*) FROM requests" + successWhere).Scan(&successful); err != nil {
		return nil, err
	}
	stats["successful_requests"] = successful

	// Failed requests
	var failed int
	failWhere := buildWhereClause(reqWhere, "status >= 400")
	if err := DB.QueryRow("SELECT COUNT(*) FROM requests" + failWhere).Scan(&failed); err != nil {
		return nil, err
	}
	stats["failed_requests"] = failed

	// Rate limit events
	var rateLimits int
	if err := DB.QueryRow("SELECT COUNT(*) FROM rate_limits" + rlWhere).Scan(&rateLimits); err != nil {
		return nil, err
	}
	stats["rate_limit_events"] = rateLimits

	// Error events
	var errors int
	if err := DB.QueryRow("SELECT COUNT(*) FROM errors" + rlWhere).Scan(&errors); err != nil {
		return nil, err
	}
	stats["error_events"] = errors

	// Average latency
	var avgLatency float64
	if err := DB.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM requests" + reqWhere).Scan(&avgLatency); err != nil {
		avgLatency = 0
	}
	stats["avg_latency"] = int(avgLatency)

	// Per-model stats
	modelStats, err := getModelStats(reqWhere)
	if err != nil {
		return nil, err
	}
	stats["models"] = modelStats

	return stats, nil
}

// buildWhereClause combines existing WHERE clause with additional condition
func buildWhereClause(existing, condition string) string {
	if existing == "" {
		return " WHERE " + condition
	}
	return existing + " AND " + condition
}

// getModelStats returns per-model statistics
func getModelStats(whereClause string) ([]map[string]interface{}, error) {
	//nolint:gosec // whereClause is safely generated internally based on controlled input
	rows, err := DB.Query(`
		SELECT provider, model, 
		       COUNT(*) as total, 
		       SUM(CASE WHEN status < 400 THEN 1 ELSE 0 END) as successful,
		       SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) as failed,
		       COALESCE(AVG(latency_ms), 0) as avg_latency
		FROM requests ` + whereClause + `
		GROUP BY provider, model
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var modelStats []map[string]interface{}
	for rows.Next() {
		var provider, model string
		var totalM, successfulM, failedM int
		var avgLatency float64
		if err := rows.Scan(&provider, &model, &totalM, &successfulM, &failedM, &avgLatency); err != nil {
			return nil, err
		}
		modelStats = append(modelStats, map[string]interface{}{
			"id":         provider + "/" + model,
			"provider":   provider,
			"model":      model,
			"total":      totalM,
			"successful": successfulM,
			"failed":     failedM,
			"latency":    int(avgLatency),
		})
	}

	return modelStats, rows.Err()
}

// RecordStatsHistory snapshots current stats into history table
func RecordStatsHistory() error {
	stats, err := GetStats()
	if err != nil {
		return err
	}

	avgLatency := 0
	if v, ok := stats["avg_latency"].(int); ok {
		avgLatency = v
	}

	_, err = DB.Exec(`
		INSERT INTO stats_history (timestamp, total_requests, successful_requests, failed_requests, rate_limit_events, error_events, avg_latency)
		VALUES (datetime('now'), ?, ?, ?, ?, ?, ?)
	`,
		stats["total_requests"],
		stats["successful_requests"],
		stats["failed_requests"],
		stats["rate_limit_events"],
		stats["error_events"],
		avgLatency,
	)

	return err
}

// GetStatsHistory returns history data points
func GetStatsHistory(points int) ([]map[string]interface{}, error) {
	return GetStatsHistoryWithPeriod(points, "")
}

// GetStatsHistoryWithPeriod returns history data points for a specific period
var GetStatsHistoryWithPeriod = func(points int, period string) ([]map[string]interface{}, error) {
	whereClause := periodToWhereClause(period)

	//nolint:gosec // whereClause is safely generated internally based on controlled input
	q := "SELECT timestamp, total_requests, successful_requests, failed_requests, rate_limit_events, error_events, avg_latency FROM stats_history" + whereClause + " ORDER BY timestamp ASC LIMIT ?"
	rows, err := DB.Query(q, points)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return scanHistoryRows(rows)
}

// scanHistoryRows scans history rows and calculates deltas
func scanHistoryRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	var history []map[string]interface{}
	var prevTotal, prevSuccessful, prevFailed, prevRateLimits, prevErrors int

	for rows.Next() {
		var timestamp string
		var total, successful, failed, rateLimits, errors int
		var avgLatency float64
		if err := rows.Scan(&timestamp, &total, &successful, &failed, &rateLimits, &errors, &avgLatency); err != nil {
			return nil, err
		}

		history = append(history, map[string]interface{}{
			"timestamp":           timestamp,
			"total_requests":      total - prevTotal,
			"successful_requests": successful - prevSuccessful,
			"failed_requests":     failed - prevFailed,
			"rate_limit_events":   rateLimits - prevRateLimits,
			"error_events":        errors - prevErrors,
			"avg_latency":         int(avgLatency),
		})

		prevTotal = total
		prevSuccessful = successful
		prevFailed = failed
		prevRateLimits = rateLimits
		prevErrors = errors
	}

	return history, rows.Err()
}

// Close gracefully shuts down the DB worker pool and closes connection
func Close() error {
	if initialized {
		close(dbChan)
		wg.Wait()
	}
	if DB != nil {
		return DB.Close()
	}
	return nil
}

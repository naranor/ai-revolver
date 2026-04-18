package db

import (
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Initialize in-memory database for tests
	if err := InitDB(":memory:"); err != nil {
		panic(err)
	}
	m.Run()
}

func TestLogRequest(t *testing.T) {
	LogRequest("test-provider", "test-model", 200, 150, false)
	time.Sleep(50 * time.Millisecond) // Wait for async write

	// Verify it was recorded
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM requests WHERE provider = ?", "test-provider").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("Expected 1 request, got %d", count)
	}
}

func TestLogRateLimit(t *testing.T) {
	resetTime := time.Now().Add(60 * time.Second)
	LogRateLimit("test-provider", resetTime)
	time.Sleep(50 * time.Millisecond)

	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM rate_limits WHERE provider = ?", "test-provider").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Errorf("Expected at least 1 rate limit event, got %d", count)
	}
}

func TestLogError(t *testing.T) {
	LogError("test-provider", "test_error", "Test error message")
	time.Sleep(50 * time.Millisecond)

	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM errors WHERE provider = ? AND error_type = ?", "test-provider", "test_error").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Errorf("Expected at least 1 error, got %d", count)
	}
}

func TestGetStats(t *testing.T) {
	// Add some test data
	LogRequest("provider1", "model1", 200, 100, false)
	LogRequest("provider1", "model1", 200, 200, false)
	LogRequest("provider1", "model2", 500, 50, false)
	time.Sleep(50 * time.Millisecond) // Wait for all async writes

	stats, err := GetStats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats["total_requests"] == nil {
		t.Error("Expected total_requests in stats")
	}
	if stats["successful_requests"] == nil {
		t.Error("Expected successful_requests in stats")
	}
	if stats["failed_requests"] == nil {
		t.Error("Expected failed_requests in stats")
	}
	if stats["avg_latency"] == nil {
		t.Error("Expected avg_latency in stats")
	}
	if stats["models"] == nil {
		t.Error("Expected models in stats")
	}
}

func TestGetStatsWithPeriod(t *testing.T) {
	periods := []string{"", "hour", "day", "week", "month"}

	for _, period := range periods {
		stats, err := GetStatsWithPeriod(period)
		if err != nil {
			t.Errorf("Failed to get stats for period '%s': %v", period, err)
		}
		if stats == nil {
			t.Errorf("Expected stats for period '%s'", period)
		}
	}
}

func TestRecordStatsHistory(t *testing.T) {
	err := RecordStatsHistory()
	if err != nil {
		t.Fatalf("Failed to record stats history: %v", err)
	}

	var count int
	err = DB.QueryRow("SELECT COUNT(*) FROM stats_history").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Errorf("Expected at least 1 history record, got %d", count)
	}
}

func TestGetStatsHistory(t *testing.T) {
	history, err := GetStatsHistory(10)
	if err != nil {
		t.Fatalf("Failed to get stats history: %v", err)
	}

	if history == nil {
		t.Error("Expected non-nil history")
	}
}

func TestGetStatsHistoryWithPeriod(t *testing.T) {
	periods := []string{"", "hour", "day", "week", "month"}

	for _, period := range periods {
		history, err := GetStatsHistoryWithPeriod(10, period)
		if err != nil {
			t.Errorf("Failed to get stats history for period '%s': %v", period, err)
		}
		if history == nil {
			t.Errorf("Expected non-nil history for period '%s'", period)
		}
	}
}

func TestPeriodToWhereClause(t *testing.T) {
	tests := []struct {
		period   string
		expected string
	}{
		{"", ""},
		{"hour", " WHERE timestamp >= datetime('now', '-1 hour')"},
		{"day", " WHERE timestamp >= datetime('now', '-1 day')"},
		{"week", " WHERE timestamp >= datetime('now', '-7 days')"},
		{"month", " WHERE timestamp >= datetime('now', '-30 days')"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		result := periodToWhereClause(tt.period)
		if result != tt.expected {
			t.Errorf("periodToWhereClause(%q) = %q, want %q", tt.period, result, tt.expected)
		}
	}
}

func TestBuildWhereClause(t *testing.T) {
	tests := []struct {
		existing  string
		condition string
		expected  string
	}{
		{"", "status < 400", " WHERE status < 400"},
		{" WHERE timestamp > '2024'", "status < 400", " WHERE timestamp > '2024' AND status < 400"},
	}

	for _, tt := range tests {
		result := buildWhereClause(tt.existing, tt.condition)
		if result != tt.expected {
			t.Errorf("buildWhereClause(%q, %q) = %q, want %q", tt.existing, tt.condition, result, tt.expected)
		}
	}
}

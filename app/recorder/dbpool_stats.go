package main

import (
	"context"
	"database/sql"
	"time"
)

// StartDBPoolStatsReporter periodically reads sql.DB.Stats() and reports via hooks.
// Returns a stop function to terminate the reporter.
func StartDBPoolStatsReporter(parent context.Context, db *sql.DB, interval time.Duration) (stop func()) {
	if db == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = 10 * time.Second
	}

	ctx, cancel := context.WithCancel(parent)

	reportOnce := func() {
		s := db.Stats()
		ReportDBPoolStats(DBPoolStats{
			Open:         s.OpenConnections,
			InUse:        s.InUse,
			Idle:         s.Idle,
			WaitCount:    s.WaitCount,
			WaitDuration: s.WaitDuration,
		})
	}

	// Report immediately once
	reportOnce()

	tk := time.NewTicker(interval)
	go func() {
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				reportOnce()
			}
		}
	}()

	return cancel
}

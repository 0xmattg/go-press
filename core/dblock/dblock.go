// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package dblock provides PostgreSQL transaction-scoped advisory locks for
// "check, then write" sequences that have no database constraint to lean on,
// such as picking a free slug or allocating a group ID.
//
// Locks are keyed by strings that the caller builds from the resource being
// protected (for example the table name, content type, and slug), hashed to
// the 64-bit key PostgreSQL expects. They are released automatically when the
// transaction commits or rolls back. On databases without advisory locks, such
// as the SQLite-backed unit tests, locking is a no-op.
package dblock

import (
	"hash/fnv"
	"sort"
	"sync"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// supported caches the capability probe per GORM configuration, which every
// session and transaction derived from one connection pool shares.
var supported sync.Map // *gorm.Config -> bool

// XactLock acquires a transaction-scoped advisory lock for every key, waiting
// for concurrent holders to finish. Keys are locked in a stable order so
// callers that lock several keys cannot deadlock each other.
//
// tx must be a transaction: outside one, PostgreSQL releases the lock as soon
// as the statement ends and it protects nothing.
func XactLock(tx *gorm.DB, keys ...string) error {
	if tx == nil || len(keys) == 0 || !advisoryLocksSupported(tx) {
		return nil
	}
	ids := make([]int64, 0, len(keys))
	seen := make(map[int64]bool, len(keys))
	for _, key := range keys {
		id := Key(key)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", id).Error; err != nil {
			return err
		}
	}
	return nil
}

// Key hashes a lock name to the signed 64-bit key used by PostgreSQL. A hash
// collision only makes two unrelated writers wait for each other briefly.
func Key(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return int64(h.Sum64())
}

func advisoryLocksSupported(db *gorm.DB) bool {
	if db.Dialector == nil || db.Dialector.Name() != "postgres" || db.Config == nil {
		return false
	}
	if cached, ok := supported.Load(db.Config); ok {
		return cached.(bool)
	}
	var found int
	quiet := db.Session(&gorm.Session{Logger: gormlogger.Discard})
	ok := quiet.Raw("SELECT 1 FROM pg_catalog.pg_proc WHERE proname = 'pg_advisory_xact_lock' LIMIT 1").Scan(&found).Error == nil && found == 1
	supported.Store(db.Config, ok)
	return ok
}

package gopressanalytics

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TrendPoint struct {
	Day            string `json:"day"`
	PageViews      int64  `json:"page_views"`
	UniqueVisitors int64  `json:"unique_visitors"`
}

type TopPage struct {
	Path           string `json:"path"`
	Title          string `json:"title"`
	PageViews      int64  `json:"page_views"`
	UniqueVisitors int64  `json:"unique_visitors"`
}

type CountryPoint struct {
	Country        string `json:"country"`
	PageViews      int64  `json:"page_views"`
	UniqueVisitors int64  `json:"unique_visitors"`
}

type Summary struct {
	Days           int            `json:"days"`
	PageViews      int64          `json:"page_views"`
	UniqueVisitors int64          `json:"unique_visitors"`
	NewVisitors    int64          `json:"new_visitors"`
	Sessions       int64          `json:"sessions"`
	Trend          []TrendPoint   `json:"trend"`
	TopPages       []TopPage      `json:"top_pages"`
	Countries      []CountryPoint `json:"countries"`
	GeneratedAt    time.Time      `json:"generated_at"`
}

type SummaryStore interface {
	Summary(ctx context.Context, days int, loc *time.Location) (Summary, error)
}

type EventQueryRow struct {
	OccurredAt     time.Time `json:"occurred_at"`
	NormalizedPath string    `json:"path"`
	IPAddress      string    `json:"ip_address"`
	Country        string    `json:"country"`
	UserAgent      string    `json:"user_agent"`
	DeviceType     string    `json:"device_type"`
	Platform       string    `json:"platform"`
	Browser        string    `json:"browser"`
	OS             string    `json:"os"`
	ReferrerHost   string    `json:"referrer_host"`
	Source         string    `json:"source"`
	Medium         string    `json:"medium"`
	StatusCode     int       `json:"status_code"`
}

type DataQueryStore interface {
	RecentEventRows(ctx context.Context, page, limit int) ([]EventQueryRow, bool, error)
}

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) AutoMigrate() error {
	if err := r.db.AutoMigrate(
		&Event{},
		&Visitor{},
		&Session{},
		&VisitorDay{},
		&PageVisitorDay{},
		&DailyMetric{},
		&DailyPageMetric{},
		&DailyDimensionMetric{},
		&DailyDimensionVisitor{},
	); err != nil {
		return err
	}
	return r.ensureUniqueIndexes()
}

type uniqueIndexSpec struct {
	table   string
	logical string
	columns []string
}

func (r *Repository) ensureUniqueIndexes() error {
	for _, spec := range analyticsUniqueIndexSpecs() {
		exists, err := r.hasUniqueIndex(spec)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := r.createUniqueIndex(spec); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) hasUniqueIndex(spec uniqueIndexSpec) (bool, error) {
	var exists bool
	err := r.db.Raw(`
SELECT EXISTS (
	SELECT 1
	FROM pg_index i
	WHERE i.indrelid = to_regclass(?)
	  AND i.indisunique
	  AND (
		SELECT string_agg(a.attname, ',' ORDER BY idxkey.ordinality)
		FROM unnest(i.indkey) WITH ORDINALITY AS idxkey(attnum, ordinality)
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = idxkey.attnum
	  ) = ?
)`, spec.table, strings.Join(spec.columns, ",")).Scan(&exists).Error
	return exists, err
}

func (r *Repository) createUniqueIndex(spec uniqueIndexSpec) error {
	indexName := analyticsUniqueIndexName(spec)
	quotedColumns := make([]string, 0, len(spec.columns))
	for _, column := range spec.columns {
		quotedColumns = append(quotedColumns, quoteIdentifier(column))
	}
	sql := fmt.Sprintf(
		"CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s)",
		quoteIdentifier(indexName),
		quoteIdentifier(spec.table),
		strings.Join(quotedColumns, ", "),
	)
	return r.db.Exec(sql).Error
}

func analyticsUniqueIndexSpecs() []uniqueIndexSpec {
	return []uniqueIndexSpec{
		{
			table: Event{}.TableName(), logical: "event_uuid",
			columns: []string{"event_uuid"},
		},
		{
			table: Visitor{}.TableName(), logical: "visitor_hash",
			columns: []string{"visitor_hash"},
		},
		{
			table: Session{}.TableName(), logical: "session_hash",
			columns: []string{"session_hash"},
		},
		{
			table: VisitorDay{}.TableName(), logical: "visitor_day",
			columns: []string{"day", "visitor_hash", "language"},
		},
		{
			table: PageVisitorDay{}.TableName(), logical: "page_visitor_day",
			columns: []string{"day", "path_hash", "visitor_hash", "language"},
		},
		{
			table: DailyMetric{}.TableName(), logical: "daily",
			columns: []string{"day", "language"},
		},
		{
			table: DailyPageMetric{}.TableName(), logical: "daily_page",
			columns: []string{"day", "path_hash", "language"},
		},
		{
			table: DailyDimensionMetric{}.TableName(), logical: "daily_dimension",
			columns: []string{"day", "dimension_type", "dimension_value", "language"},
		},
		{
			table: DailyDimensionVisitor{}.TableName(), logical: "daily_dimension_visitor",
			columns: []string{"day", "dimension_type", "dimension_value", "visitor_hash", "language"},
		},
	}
}

func analyticsUniqueIndexName(spec uniqueIndexSpec) string {
	sum := sha1.Sum([]byte(spec.table + ":" + strings.Join(spec.columns, ",")))
	return fmt.Sprintf("uidx_gpa_%s_%s", spec.logical, hex.EncodeToString(sum[:4]))
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (r *Repository) RecordBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		unrecorded, err := filterUnrecordedEvents(tx, events)
		if err != nil || len(unrecorded) == 0 {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_uuid"}},
			DoNothing: true,
		}).CreateInBatches(&unrecorded, 500).Error; err != nil {
			return err
		}
		return recordAggregates(tx, aggregateEvents(unrecorded))
	})
}

type visitorAggregate struct {
	first Event
	last  Event
}

type sessionAggregate struct {
	first     Event
	last      Event
	pageViews int64
}

type dailyKey struct {
	day      time.Time
	language string
}

type dailyAggregate struct {
	event          Event
	pageViews      int64
	uniqueVisitors int64
	newVisitors    int64
	sessions       int64
}

type visitorDayKey struct {
	dailyKey
	visitorHash string
}

type pageKey struct {
	dailyKey
	pathHash string
}

type pageAggregate struct {
	event          Event
	pageViews      int64
	uniqueVisitors int64
}

type pageVisitorDayKey struct {
	pageKey
	visitorHash string
}

type dimensionKey struct {
	dailyKey
	dimensionType  string
	dimensionValue string
}

type dimensionVisitorKey struct {
	dimensionKey
	visitorHash string
}

type dimensionAggregate struct {
	event          Event
	pageViews      int64
	uniqueVisitors int64
}

type eventAggregates struct {
	visitors          map[string]*visitorAggregate
	sessions          map[string]*sessionAggregate
	visitorDays       map[visitorDayKey]Event
	daily             map[dailyKey]*dailyAggregate
	pageVisitorDays   map[pageVisitorDayKey]Event
	pages             map[pageKey]*pageAggregate
	dimensions        map[dimensionKey]*dimensionAggregate
	dimensionVisitors map[dimensionVisitorKey]Event
}

func filterUnrecordedEvents(tx *gorm.DB, events []Event) ([]Event, error) {
	unique := make([]Event, 0, len(events))
	seen := make(map[string]struct{}, len(events))
	uuids := make([]string, 0, len(events))
	for i := range events {
		if events[i].EventUUID == "" {
			continue
		}
		if _, exists := seen[events[i].EventUUID]; exists {
			continue
		}
		seen[events[i].EventUUID] = struct{}{}
		unique = append(unique, events[i])
		uuids = append(uuids, events[i].EventUUID)
	}
	if len(unique) == 0 {
		return nil, nil
	}

	var existing []string
	if err := tx.Model(&Event{}).
		Where("event_uuid IN ?", uuids).
		Pluck("event_uuid", &existing).Error; err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		return unique, nil
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, eventUUID := range existing {
		existingSet[eventUUID] = struct{}{}
	}
	filtered := unique[:0]
	for i := range unique {
		if _, exists := existingSet[unique[i].EventUUID]; !exists {
			filtered = append(filtered, unique[i])
		}
	}
	return filtered, nil
}

func aggregateEvents(events []Event) *eventAggregates {
	result := &eventAggregates{
		visitors:          make(map[string]*visitorAggregate),
		sessions:          make(map[string]*sessionAggregate),
		visitorDays:       make(map[visitorDayKey]Event),
		daily:             make(map[dailyKey]*dailyAggregate),
		pageVisitorDays:   make(map[pageVisitorDayKey]Event),
		pages:             make(map[pageKey]*pageAggregate),
		dimensions:        make(map[dimensionKey]*dimensionAggregate),
		dimensionVisitors: make(map[dimensionVisitorKey]Event),
	}
	for i := range events {
		event := events[i]
		visitor := result.visitors[event.VisitorHash]
		if visitor == nil {
			result.visitors[event.VisitorHash] = &visitorAggregate{first: event, last: event}
		} else {
			if event.OccurredAt.Before(visitor.first.OccurredAt) {
				visitor.first = event
			}
			if event.OccurredAt.After(visitor.last.OccurredAt) {
				visitor.last = event
			}
		}

		session := result.sessions[event.SessionHash]
		if session == nil {
			result.sessions[event.SessionHash] = &sessionAggregate{
				first: event, last: event, pageViews: 1,
			}
		} else {
			session.pageViews++
			if event.OccurredAt.Before(session.first.OccurredAt) {
				session.first = event
			}
			if event.OccurredAt.After(session.last.OccurredAt) {
				session.last = event
			}
		}

		day := dailyKey{day: event.Day, language: event.Language}
		daily := result.daily[day]
		if daily == nil {
			result.daily[day] = &dailyAggregate{event: event, pageViews: 1}
		} else {
			daily.pageViews++
			if event.OccurredAt.After(daily.event.OccurredAt) {
				daily.event = event
			}
		}
		result.visitorDays[visitorDayKey{dailyKey: day, visitorHash: event.VisitorHash}] = event

		page := pageKey{dailyKey: day, pathHash: event.PathHash}
		pageMetric := result.pages[page]
		if pageMetric == nil {
			result.pages[page] = &pageAggregate{event: event, pageViews: 1}
		} else {
			pageMetric.pageViews++
			if event.OccurredAt.After(pageMetric.event.OccurredAt) {
				pageMetric.event = event
			}
		}
		result.pageVisitorDays[pageVisitorDayKey{
			pageKey: page, visitorHash: event.VisitorHash,
		}] = event

		if country := normalizeCountryCode(event.Country); country != "" {
			dimension := dimensionKey{
				dailyKey:       day,
				dimensionType:  "country",
				dimensionValue: country,
			}
			metric := result.dimensions[dimension]
			if metric == nil {
				result.dimensions[dimension] = &dimensionAggregate{event: event, pageViews: 1}
			} else {
				metric.pageViews++
				if event.OccurredAt.After(metric.event.OccurredAt) {
					metric.event = event
				}
			}
			result.dimensionVisitors[dimensionVisitorKey{
				dimensionKey: dimension,
				visitorHash:  event.VisitorHash,
			}] = event
		}
	}
	return result
}

func recordAggregates(tx *gorm.DB, batch *eventAggregates) error {
	newVisitorSessions := make(map[string]int64)
	for visitorHash, aggregate := range batch.visitors {
		event := aggregate.first
		visitor := Visitor{
			VisitorHash:   visitorHash,
			FirstSeenAt:   event.OccurredAt,
			LastSeenAt:    aggregate.last.OccurredAt,
			FirstSource:   event.Source,
			FirstMedium:   event.Medium,
			FirstCampaign: event.Campaign,
			FirstLanding:  event.NormalizedPath,
			FirstCountry:  event.Country,
		}
		visitorInsert := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "visitor_hash"}},
			DoNothing: true,
		}).Create(&visitor)
		if visitorInsert.Error != nil {
			return visitorInsert.Error
		}
		if visitorInsert.RowsAffected > 0 {
			key := dailyKey{day: event.Day, language: event.Language}
			batch.daily[key].newVisitors++
			continue
		}
		if err := tx.Model(&Visitor{}).
			Where("visitor_hash = ?", visitorHash).
			Update("last_seen_at", aggregate.last.OccurredAt).Error; err != nil {
			return err
		}
	}

	for sessionHash, aggregate := range batch.sessions {
		event := aggregate.first
		last := aggregate.last
		session := Session{
			SessionHash:   sessionHash,
			VisitorHash:   event.VisitorHash,
			StartedAt:     event.OccurredAt,
			LastSeenAt:    last.OccurredAt,
			EntryPath:     event.NormalizedPath,
			ExitPath:      last.NormalizedPath,
			PageViewCount: aggregate.pageViews,
			Source:        event.Source,
			Medium:        event.Medium,
			Campaign:      event.Campaign,
			IPAddress:     event.IPAddress,
			IPHash:        event.IPHash,
			DeviceType:    event.DeviceType,
			Platform:      event.Platform,
			DeviceVendor:  event.DeviceVendor,
			DeviceModel:   event.DeviceModel,
			Browser:       event.Browser,
			OS:            event.OS,
			Country:       event.Country,
			IsBounce:      aggregate.pageViews == 1,
		}
		sessionInsert := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "session_hash"}},
			DoNothing: true,
		}).Create(&session)
		if sessionInsert.Error != nil {
			return sessionInsert.Error
		}
		if sessionInsert.RowsAffected > 0 {
			newVisitorSessions[event.VisitorHash]++
			key := dailyKey{day: event.Day, language: event.Language}
			batch.daily[key].sessions++
			continue
		}
		if err := tx.Model(&Session{}).
			Where("session_hash = ?", sessionHash).
			Updates(map[string]interface{}{
				"last_seen_at":    last.OccurredAt,
				"exit_path":       last.NormalizedPath,
				"page_view_count": gorm.Expr("page_view_count + ?", aggregate.pageViews),
				"is_bounce":       false,
			}).Error; err != nil {
			return err
		}
	}
	for visitorHash, sessions := range newVisitorSessions {
		if err := tx.Model(&Visitor{}).
			Where("visitor_hash = ?", visitorHash).
			UpdateColumn("visit_count", gorm.Expr("visit_count + ?", sessions)).Error; err != nil {
			return err
		}
	}

	visitorDaysByMetric := make(map[dailyKey][]VisitorDay)
	for key := range batch.visitorDays {
		visitorDaysByMetric[key.dailyKey] = append(visitorDaysByMetric[key.dailyKey], VisitorDay{
			Day: key.day, VisitorHash: key.visitorHash, Language: key.language,
		})
	}
	for key, visitorDays := range visitorDaysByMetric {
		insert := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "day"}, {Name: "visitor_hash"}, {Name: "language"},
			},
			DoNothing: true,
		}).CreateInBatches(&visitorDays, 500)
		if insert.Error != nil {
			return insert.Error
		}
		batch.daily[key].uniqueVisitors += insert.RowsAffected
	}

	for key, aggregate := range batch.daily {
		event := aggregate.event
		daily := DailyMetric{
			Day:            key.day,
			Language:       key.language,
			PageViews:      aggregate.pageViews,
			UniqueVisitors: aggregate.uniqueVisitors,
			NewVisitors:    aggregate.newVisitors,
			Sessions:       aggregate.sessions,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "day"}, {Name: "language"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"page_views":      addColumn(DailyMetric{}.TableName(), "page_views", aggregate.pageViews),
				"unique_visitors": addColumn(DailyMetric{}.TableName(), "unique_visitors", aggregate.uniqueVisitors),
				"new_visitors":    addColumn(DailyMetric{}.TableName(), "new_visitors", aggregate.newVisitors),
				"sessions":        addColumn(DailyMetric{}.TableName(), "sessions", aggregate.sessions),
				"updated_at":      event.OccurredAt,
			}),
		}).Create(&daily).Error; err != nil {
			return err
		}
	}

	pageVisitorDaysByMetric := make(map[pageKey][]PageVisitorDay)
	for key, event := range batch.pageVisitorDays {
		pageVisitorDaysByMetric[key.pageKey] = append(pageVisitorDaysByMetric[key.pageKey], PageVisitorDay{
			Day:            key.day,
			NormalizedPath: event.NormalizedPath,
			PathHash:       key.pathHash,
			VisitorHash:    key.visitorHash,
			Language:       key.language,
		})
	}
	for key, pageVisitorDays := range pageVisitorDaysByMetric {
		insert := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "day"}, {Name: "path_hash"}, {Name: "visitor_hash"}, {Name: "language"},
			},
			DoNothing: true,
		}).CreateInBatches(&pageVisitorDays, 500)
		if insert.Error != nil {
			return insert.Error
		}
		batch.pages[key].uniqueVisitors += insert.RowsAffected
	}

	for key, aggregate := range batch.pages {
		event := aggregate.event
		page := DailyPageMetric{
			Day:            key.day,
			NormalizedPath: event.NormalizedPath,
			PathHash:       key.pathHash,
			Language:       key.language,
			PageTitle:      event.PageTitle,
			ContentID:      event.ContentID,
			ContentType:    event.ContentType,
			PageViews:      aggregate.pageViews,
			UniqueVisitors: aggregate.uniqueVisitors,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "day"}, {Name: "path_hash"}, {Name: "language"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"page_views":      addColumn(DailyPageMetric{}.TableName(), "page_views", aggregate.pageViews),
				"unique_visitors": addColumn(DailyPageMetric{}.TableName(), "unique_visitors", aggregate.uniqueVisitors),
				"page_title":      event.PageTitle,
				"content_id":      event.ContentID,
				"content_type":    event.ContentType,
				"updated_at":      event.OccurredAt,
			}),
		}).Create(&page).Error; err != nil {
			return err
		}
	}

	dimensionVisitorsByMetric := make(map[dimensionKey][]DailyDimensionVisitor)
	for key := range batch.dimensionVisitors {
		dimensionVisitorsByMetric[key.dimensionKey] = append(dimensionVisitorsByMetric[key.dimensionKey], DailyDimensionVisitor{
			Day:            key.day,
			DimensionType:  key.dimensionType,
			DimensionValue: key.dimensionValue,
			VisitorHash:    key.visitorHash,
			Language:       key.language,
		})
	}
	for key, dimensionVisitors := range dimensionVisitorsByMetric {
		insert := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "day"}, {Name: "dimension_type"}, {Name: "dimension_value"}, {Name: "visitor_hash"}, {Name: "language"},
			},
			DoNothing: true,
		}).CreateInBatches(&dimensionVisitors, 500)
		if insert.Error != nil {
			return insert.Error
		}
		if metric := batch.dimensions[key]; metric != nil {
			metric.uniqueVisitors = insert.RowsAffected
		}
	}

	for key, aggregate := range batch.dimensions {
		event := aggregate.event
		dimension := DailyDimensionMetric{
			Day:             key.day,
			DimensionType:   key.dimensionType,
			DimensionValue:  key.dimensionValue,
			Language:        key.language,
			PageViews:       aggregate.pageViews,
			UniqueVisitors:  aggregate.uniqueVisitors,
			BouncedSessions: 0,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "day"}, {Name: "dimension_type"}, {Name: "dimension_value"}, {Name: "language"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"page_views":      addColumn(DailyDimensionMetric{}.TableName(), "page_views", aggregate.pageViews),
				"unique_visitors": addColumn(DailyDimensionMetric{}.TableName(), "unique_visitors", aggregate.uniqueVisitors),
				"updated_at":      event.OccurredAt,
			}),
		}).Create(&dimension).Error; err != nil {
			return err
		}
	}
	return nil
}

func addColumn(table, column string, value int64) clause.Expr {
	return gorm.Expr("? + ?", clause.Column{Table: table, Name: column}, value)
}

func (r *Repository) RecentEventRows(ctx context.Context, page, limit int) ([]EventQueryRow, bool, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var rows []EventQueryRow
	err := r.db.WithContext(ctx).Model(&Event{}).
		Select("occurred_at, normalized_path, ip_address, country, user_agent, device_type, platform, browser, os, referrer_host, source, medium, status_code").
		Order("occurred_at DESC, id DESC").
		Offset((page - 1) * limit).
		Limit(limit + 1).
		Scan(&rows).Error
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}

func (r *Repository) Summary(ctx context.Context, days int, loc *time.Location) (Summary, error) {
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))

	result := Summary{Days: days, GeneratedAt: time.Now().UTC()}
	var totals struct {
		PageViews   int64
		NewVisitors int64
		Sessions    int64
	}
	if err := r.db.WithContext(ctx).Model(&DailyMetric{}).
		Select("COALESCE(SUM(page_views), 0) AS page_views, COALESCE(SUM(new_visitors), 0) AS new_visitors, COALESCE(SUM(sessions), 0) AS sessions").
		Where("day >= ? AND day <= ?", startDay, now).
		Scan(&totals).Error; err != nil {
		return Summary{}, err
	}
	result.PageViews = totals.PageViews
	result.NewVisitors = totals.NewVisitors
	result.Sessions = totals.Sessions
	if err := r.db.WithContext(ctx).Model(&VisitorDay{}).
		Where("day >= ? AND day <= ?", startDay, now).
		Distinct("visitor_hash").
		Count(&result.UniqueVisitors).Error; err != nil {
		return Summary{}, err
	}

	var pageViewRows []struct {
		Day       time.Time
		PageViews int64
	}
	if err := r.db.WithContext(ctx).Model(&DailyMetric{}).
		Select("day, SUM(page_views) AS page_views").
		Where("day >= ? AND day <= ?", startDay, now).
		Group("day").
		Order("day ASC").
		Scan(&pageViewRows).Error; err != nil {
		return Summary{}, err
	}
	var visitorRows []struct {
		Day            time.Time
		UniqueVisitors int64
	}
	if err := r.db.WithContext(ctx).Model(&VisitorDay{}).
		Select("day, COUNT(DISTINCT visitor_hash) AS unique_visitors").
		Where("day >= ? AND day <= ?", startDay, now).
		Group("day").
		Order("day ASC").
		Scan(&visitorRows).Error; err != nil {
		return Summary{}, err
	}
	result.Trend = mergeTrend(days, startDay, pageViewRows, visitorRows)

	var pageRows []struct {
		PathHash  string
		Path      string
		Title     string
		PageViews int64
	}
	if err := r.db.WithContext(ctx).Model(&DailyPageMetric{}).
		Select("path_hash, MAX(normalized_path) AS path, MAX(page_title) AS title, SUM(page_views) AS page_views").
		Where("day >= ? AND day <= ?", startDay, now).
		Group("path_hash").
		Order("page_views DESC").
		Limit(8).
		Scan(&pageRows).Error; err != nil {
		return Summary{}, fmt.Errorf("query top pages: %w", err)
	}
	pathHashes := make([]string, 0, len(pageRows))
	for _, row := range pageRows {
		pathHashes = append(pathHashes, row.PathHash)
	}
	visitorCounts := make(map[string]int64, len(pathHashes))
	if len(pathHashes) > 0 {
		var pageVisitorRows []struct {
			PathHash       string
			UniqueVisitors int64
		}
		if err := r.db.WithContext(ctx).Model(&PageVisitorDay{}).
			Select("path_hash, COUNT(DISTINCT visitor_hash) AS unique_visitors").
			Where("day >= ? AND day <= ? AND path_hash IN ?", startDay, now, pathHashes).
			Group("path_hash").
			Scan(&pageVisitorRows).Error; err != nil {
			return Summary{}, fmt.Errorf("query top page visitors: %w", err)
		}
		for _, row := range pageVisitorRows {
			visitorCounts[row.PathHash] = row.UniqueVisitors
		}
	}
	result.TopPages = make([]TopPage, 0, len(pageRows))
	for _, row := range pageRows {
		result.TopPages = append(result.TopPages, TopPage{
			Path:           row.Path,
			Title:          row.Title,
			PageViews:      row.PageViews,
			UniqueVisitors: visitorCounts[row.PathHash],
		})
	}

	var countryPVRows []struct {
		Country   string
		PageViews int64
	}
	if err := r.db.WithContext(ctx).Model(&DailyDimensionMetric{}).
		Select("dimension_value AS country, SUM(page_views) AS page_views").
		Where("day >= ? AND day <= ? AND dimension_type = ?", startDay, now, "country").
		Group("dimension_value").
		Scan(&countryPVRows).Error; err != nil {
		return Summary{}, fmt.Errorf("query country page views: %w", err)
	}
	countryPageViews := make(map[string]int64, len(countryPVRows))
	for _, row := range countryPVRows {
		countryPageViews[row.Country] = row.PageViews
	}
	var countryVisitorRows []struct {
		Country        string
		UniqueVisitors int64
	}
	if err := r.db.WithContext(ctx).Model(&DailyDimensionVisitor{}).
		Select("dimension_value AS country, COUNT(DISTINCT visitor_hash) AS unique_visitors").
		Where("day >= ? AND day <= ? AND dimension_type = ?", startDay, now, "country").
		Group("dimension_value").
		Order("unique_visitors DESC").
		Limit(6).
		Scan(&countryVisitorRows).Error; err != nil {
		return Summary{}, fmt.Errorf("query country visitors: %w", err)
	}
	result.Countries = make([]CountryPoint, 0, len(countryVisitorRows))
	for _, row := range countryVisitorRows {
		result.Countries = append(result.Countries, CountryPoint{
			Country:        row.Country,
			PageViews:      countryPageViews[row.Country],
			UniqueVisitors: row.UniqueVisitors,
		})
	}
	return result, nil
}

func mergeTrend(
	days int,
	startDay time.Time,
	pageViewRows []struct {
		Day       time.Time
		PageViews int64
	},
	visitorRows []struct {
		Day            time.Time
		UniqueVisitors int64
	},
) []TrendPoint {
	pageViews := make(map[string]int64, len(pageViewRows))
	visitors := make(map[string]int64, len(visitorRows))
	for _, row := range pageViewRows {
		pageViews[row.Day.Format("2006-01-02")] = row.PageViews
	}
	for _, row := range visitorRows {
		visitors[row.Day.Format("2006-01-02")] = row.UniqueVisitors
	}
	trend := make([]TrendPoint, 0, days)
	for i := 0; i < days; i++ {
		day := startDay.AddDate(0, 0, i).Format("2006-01-02")
		trend = append(trend, TrendPoint{
			Day:            day,
			PageViews:      pageViews[day],
			UniqueVisitors: visitors[day],
		})
	}
	return trend
}

func (r *Repository) PurgeBefore(ctx context.Context, cutoff time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("occurred_at < ?", cutoff).Delete(&Event{}).Error; err != nil {
			return err
		}
		return tx.Where("last_seen_at < ?", cutoff).Delete(&Session{}).Error
	})
}

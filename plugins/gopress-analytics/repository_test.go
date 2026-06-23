package gopressanalytics

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm/clause"
)

func TestAddColumnQualifiesConflictTargetTable(t *testing.T) {
	expr := addColumn(DailyMetric{}.TableName(), "page_views", 3)
	if expr.SQL != "? + ?" || len(expr.Vars) != 2 {
		t.Fatalf("unexpected increment expression: %#v", expr)
	}
	column, ok := expr.Vars[0].(clause.Column)
	if !ok {
		t.Fatalf("increment variable type = %T, want clause.Column", expr.Vars[0])
	}
	if column.Table != (DailyMetric{}).TableName() || column.Name != "page_views" {
		t.Fatalf("increment column = %#v", column)
	}
	if expr.Vars[1] != int64(3) {
		t.Fatalf("increment value = %#v, want 3", expr.Vars[1])
	}
}

func TestAggregateEventsCompactsRepeatedPageViews(t *testing.T) {
	day := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	base := Event{
		EventUUID:      "event-1",
		OccurredAt:     day.Add(time.Second),
		Day:            day,
		VisitorHash:    "visitor-1",
		SessionHash:    "session-1",
		NormalizedPath: "/products/hepa",
		PathHash:       "path-1",
		Language:       "en",
	}
	events := []Event{base}
	for i := 2; i <= 3; i++ {
		event := base
		event.EventUUID = fmt.Sprintf("event-%d", i)
		event.OccurredAt = day.Add(time.Duration(i) * time.Second)
		events = append(events, event)
	}

	aggregated := aggregateEvents(events)
	if len(aggregated.visitors) != 1 || len(aggregated.sessions) != 1 {
		t.Fatalf("visitors/sessions = %d/%d, want 1/1", len(aggregated.visitors), len(aggregated.sessions))
	}
	if len(aggregated.visitorDays) != 1 || len(aggregated.pageVisitorDays) != 1 {
		t.Fatalf("visitor dimensions = %d/%d, want 1/1", len(aggregated.visitorDays), len(aggregated.pageVisitorDays))
	}
	key := dailyKey{day: day, language: "en"}
	if aggregated.daily[key].pageViews != 3 {
		t.Fatalf("daily page views = %d, want 3", aggregated.daily[key].pageViews)
	}
	page := pageKey{dailyKey: key, pathHash: "path-1"}
	if aggregated.pages[page].pageViews != 3 {
		t.Fatalf("page views = %d, want 3", aggregated.pages[page].pageViews)
	}
	if aggregated.sessions["session-1"].pageViews != 3 {
		t.Fatalf("session page views = %d, want 3", aggregated.sessions["session-1"].pageViews)
	}
}

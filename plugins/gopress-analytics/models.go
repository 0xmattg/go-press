package gopressanalytics

import (
	"time"

	"go-press/pkg/dbprefix"
)

const storageSlug = "gopress_analytics"

type Event struct {
	ID             uint      `gorm:"primaryKey"`
	EventUUID      string    `gorm:"size:48;not null;uniqueIndex:uidx_gpa_event_uuid"`
	EventType      string    `gorm:"size:32;not null;index:idx_gpa_event_type"`
	EventName      string    `gorm:"size:80"`
	OccurredAt     time.Time `gorm:"not null;index:idx_gpa_event_occurred"`
	Day            time.Time `gorm:"type:date;not null;index:idx_gpa_event_day"`
	VisitorHash    string    `gorm:"size:64;not null;index:idx_gpa_event_visitor"`
	SessionHash    string    `gorm:"size:64;not null;index:idx_gpa_event_session"`
	NormalizedPath string    `gorm:"size:1024;not null"`
	PathHash       string    `gorm:"size:64;not null;index:idx_gpa_event_path_hash"`
	PageTitle      string    `gorm:"size:500"`
	ContentID      *uint     `gorm:"index:idx_gpa_event_content"`
	ContentType    string    `gorm:"size:80;index:idx_gpa_event_content"`
	Language       string    `gorm:"size:16;not null;default:und;index:idx_gpa_event_language"`
	ReferrerHost   string    `gorm:"size:255"`
	Source         string    `gorm:"size:120"`
	Medium         string    `gorm:"size:120"`
	Campaign       string    `gorm:"size:255"`
	IPAddress      string    `gorm:"size:45;index:idx_gpa_event_ip"`
	IPHash         string    `gorm:"size:64;index:idx_gpa_event_ip_hash"`
	UserAgent      string    `gorm:"size:1024"`
	DeviceType     string    `gorm:"size:24"`
	Platform       string    `gorm:"size:48"`
	DeviceVendor   string    `gorm:"size:80"`
	DeviceModel    string    `gorm:"size:120"`
	Browser        string    `gorm:"size:48"`
	OS             string    `gorm:"size:48"`
	Country        string    `gorm:"size:8"`
	Region         string    `gorm:"size:120"`
	StatusCode     int
	DurationMS     int64
	IsBot          bool   `gorm:"not null;default:false;index:idx_gpa_event_bot"`
	Properties     string `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt      time.Time
}

func (Event) TableName() string { return dbprefix.PluginTable(storageSlug, "events") }

type Visitor struct {
	ID            uint      `gorm:"primaryKey"`
	VisitorHash   string    `gorm:"size:64;not null;uniqueIndex:uidx_gpa_visitor_hash"`
	FirstSeenAt   time.Time `gorm:"not null;index:idx_gpa_visitor_first_seen"`
	LastSeenAt    time.Time `gorm:"not null;index:idx_gpa_visitor_last_seen"`
	FirstSource   string    `gorm:"size:120"`
	FirstMedium   string    `gorm:"size:120"`
	FirstCampaign string    `gorm:"size:255"`
	FirstLanding  string    `gorm:"size:1024"`
	VisitCount    int64     `gorm:"not null;default:0"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (Visitor) TableName() string { return dbprefix.PluginTable(storageSlug, "visitors") }

type Session struct {
	ID             uint      `gorm:"primaryKey"`
	SessionHash    string    `gorm:"size:64;not null;uniqueIndex:uidx_gpa_session_hash"`
	VisitorHash    string    `gorm:"size:64;not null;index:idx_gpa_session_visitor"`
	StartedAt      time.Time `gorm:"not null;index:idx_gpa_session_started"`
	LastSeenAt     time.Time `gorm:"not null;index:idx_gpa_session_last_seen"`
	EntryPath      string    `gorm:"size:1024"`
	ExitPath       string    `gorm:"size:1024"`
	PageViewCount  int64     `gorm:"not null;default:1"`
	EngagedSeconds int64     `gorm:"not null;default:0"`
	Source         string    `gorm:"size:120"`
	Medium         string    `gorm:"size:120"`
	Campaign       string    `gorm:"size:255"`
	IPAddress      string    `gorm:"size:45;index:idx_gpa_session_ip"`
	IPHash         string    `gorm:"size:64;index:idx_gpa_session_ip_hash"`
	DeviceType     string    `gorm:"size:24"`
	Platform       string    `gorm:"size:48"`
	DeviceVendor   string    `gorm:"size:80"`
	DeviceModel    string    `gorm:"size:120"`
	Browser        string    `gorm:"size:48"`
	OS             string    `gorm:"size:48"`
	Country        string    `gorm:"size:8"`
	IsBounce       bool      `gorm:"not null;default:true"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (Session) TableName() string { return dbprefix.PluginTable(storageSlug, "sessions") }

type VisitorDay struct {
	ID          uint      `gorm:"primaryKey"`
	Day         time.Time `gorm:"type:date;not null;uniqueIndex:uidx_gpa_visitor_day"`
	VisitorHash string    `gorm:"size:64;not null;uniqueIndex:uidx_gpa_visitor_day"`
	Language    string    `gorm:"size:16;not null;default:und;uniqueIndex:uidx_gpa_visitor_day"`
	CreatedAt   time.Time
}

func (VisitorDay) TableName() string {
	return dbprefix.PluginTable(storageSlug, "visitor_days")
}

type PageVisitorDay struct {
	ID             uint      `gorm:"primaryKey"`
	Day            time.Time `gorm:"type:date;not null;uniqueIndex:uidx_gpa_page_visitor_day"`
	NormalizedPath string    `gorm:"size:1024;not null"`
	PathHash       string    `gorm:"size:64;not null;uniqueIndex:uidx_gpa_page_visitor_day"`
	VisitorHash    string    `gorm:"size:64;not null;uniqueIndex:uidx_gpa_page_visitor_day"`
	Language       string    `gorm:"size:16;not null;default:und;uniqueIndex:uidx_gpa_page_visitor_day"`
	CreatedAt      time.Time
}

func (PageVisitorDay) TableName() string {
	return dbprefix.PluginTable(storageSlug, "page_visitor_days")
}

type DailyMetric struct {
	ID              uint      `gorm:"primaryKey"`
	Day             time.Time `gorm:"type:date;not null;uniqueIndex:uidx_gpa_daily"`
	Language        string    `gorm:"size:16;not null;default:und;uniqueIndex:uidx_gpa_daily"`
	PageViews       int64     `gorm:"not null;default:0"`
	UniqueVisitors  int64     `gorm:"not null;default:0"`
	NewVisitors     int64     `gorm:"not null;default:0"`
	Sessions        int64     `gorm:"not null;default:0"`
	BouncedSessions int64     `gorm:"not null;default:0"`
	EngagedSeconds  int64     `gorm:"not null;default:0"`
	Conversions     int64     `gorm:"not null;default:0"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (DailyMetric) TableName() string { return dbprefix.PluginTable(storageSlug, "daily") }

type DailyPageMetric struct {
	ID             uint      `gorm:"primaryKey"`
	Day            time.Time `gorm:"type:date;not null;uniqueIndex:uidx_gpa_daily_page"`
	NormalizedPath string    `gorm:"size:1024;not null"`
	PathHash       string    `gorm:"size:64;not null;uniqueIndex:uidx_gpa_daily_page"`
	Language       string    `gorm:"size:16;not null;default:und;uniqueIndex:uidx_gpa_daily_page"`
	PageTitle      string    `gorm:"size:500"`
	ContentID      *uint     `gorm:"index:idx_gpa_daily_page_content"`
	ContentType    string    `gorm:"size:80;index:idx_gpa_daily_page_content"`
	PageViews      int64     `gorm:"not null;default:0"`
	UniqueVisitors int64     `gorm:"not null;default:0"`
	Entrances      int64     `gorm:"not null;default:0"`
	Exits          int64     `gorm:"not null;default:0"`
	Bounces        int64     `gorm:"not null;default:0"`
	EngagedSeconds int64     `gorm:"not null;default:0"`
	Conversions    int64     `gorm:"not null;default:0"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (DailyPageMetric) TableName() string {
	return dbprefix.PluginTable(storageSlug, "daily_pages")
}

// DailyDimensionMetric reserves the aggregate shape used by later releases for
// source, medium, device, browser, OS, country, and campaign breakdowns.
type DailyDimensionMetric struct {
	ID              uint      `gorm:"primaryKey"`
	Day             time.Time `gorm:"type:date;not null;uniqueIndex:uidx_gpa_daily_dimension"`
	DimensionType   string    `gorm:"size:40;not null;uniqueIndex:uidx_gpa_daily_dimension"`
	DimensionValue  string    `gorm:"size:255;not null;uniqueIndex:uidx_gpa_daily_dimension"`
	Language        string    `gorm:"size:16;not null;default:und;uniqueIndex:uidx_gpa_daily_dimension"`
	PageViews       int64     `gorm:"not null;default:0"`
	UniqueVisitors  int64     `gorm:"not null;default:0"`
	Sessions        int64     `gorm:"not null;default:0"`
	BouncedSessions int64     `gorm:"not null;default:0"`
	EngagedSeconds  int64     `gorm:"not null;default:0"`
	Conversions     int64     `gorm:"not null;default:0"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (DailyDimensionMetric) TableName() string {
	return dbprefix.PluginTable(storageSlug, "daily_dimensions")
}

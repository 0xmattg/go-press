package gopressanalytics

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	geoIPFileRelPath = "plugins/gopress-analytics/data/dbip-country-lite.csv.gz"
	geoIPMaxBytes    = 100 << 20
)

type GeoIPStatus struct {
	Available bool   `json:"available"`
	Path      string `json:"path"`
	Source    string `json:"source"`
	UpdatedAt string `json:"updated_at"`
	Records   int    `json:"records"`
	Error     string `json:"error,omitempty"`
}

type geoIPRange struct {
	start   netip.Addr
	end     netip.Addr
	country string
}

type geoIPDatabase struct {
	mu      sync.RWMutex
	path    string
	source  string
	ranges  []geoIPRange
	modTime time.Time
	lastErr string
	sources []string
}

func newGeoIPDatabase(path string) *geoIPDatabase {
	return &geoIPDatabase{path: path}
}

func defaultGeoIPSources(now time.Time) []string {
	sources := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		month := now.AddDate(0, -i, 0).Format("2006-01")
		sources = append(sources, fmt.Sprintf("https://download.db-ip.com/free/dbip-country-lite-%s.csv.gz", month))
	}
	return sources
}

func (g *geoIPDatabase) Load() error {
	ranges, err := loadGeoIPRanges(g.path)
	if err != nil {
		g.mu.Lock()
		g.ranges = nil
		g.lastErr = err.Error()
		g.mu.Unlock()
		return err
	}
	info, _ := os.Stat(g.path)
	g.mu.Lock()
	g.ranges = ranges
	if info != nil {
		g.modTime = info.ModTime()
	}
	g.lastErr = ""
	g.mu.Unlock()
	return nil
}

func (g *geoIPDatabase) LookupCountry(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return ""
	}
	g.mu.RLock()
	ranges := g.ranges
	g.mu.RUnlock()
	if len(ranges) == 0 {
		return ""
	}
	i := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].start.Compare(addr) > 0
	})
	if i == 0 {
		return ""
	}
	match := ranges[i-1]
	if match.end.Compare(addr) >= 0 {
		return match.country
	}
	return ""
}

func (g *geoIPDatabase) Status() GeoIPStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	status := GeoIPStatus{
		Available: len(g.ranges) > 0,
		Path:      g.path,
		Source:    g.source,
		Records:   len(g.ranges),
		Error:     g.lastErr,
	}
	if !g.modTime.IsZero() {
		status.UpdatedAt = g.modTime.Format("2006-01-02 15:04:05")
	}
	return status
}

func (g *geoIPDatabase) Update(ctx context.Context) (GeoIPStatus, error) {
	sources := g.sources
	if len(sources) == 0 {
		sources = defaultGeoIPSources(time.Now())
	}
	var lastErr error
	for _, source := range sources {
		status, err := g.downloadFrom(ctx, source)
		if err == nil {
			return status, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no GeoIP download sources configured")
	}
	g.mu.Lock()
	g.lastErr = lastErr.Error()
	g.mu.Unlock()
	return g.Status(), lastErr
}

func (g *geoIPDatabase) downloadFrom(ctx context.Context, source string) (GeoIPStatus, error) {
	if path, ok := strings.CutPrefix(source, "file://"); ok {
		file, err := os.Open(path)
		if err != nil {
			return GeoIPStatus{}, err
		}
		defer file.Close()
		return g.installFromReader(file, source)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return GeoIPStatus{}, err
	}
	req.Header.Set("User-Agent", "GoPress Analytics GeoIP Updater")
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return GeoIPStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return GeoIPStatus{}, fmt.Errorf("GeoIP download returned %s", resp.Status)
	}
	return g.installFromReader(resp.Body, source)
}

func (g *geoIPDatabase) installFromReader(r io.Reader, source string) (GeoIPStatus, error) {
	if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
		return GeoIPStatus{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(g.path), "geoip-*.csv.gz")
	if err != nil {
		return GeoIPStatus{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, io.LimitReader(r, geoIPMaxBytes)); err != nil {
		tmp.Close()
		return GeoIPStatus{}, err
	}
	if err := tmp.Close(); err != nil {
		return GeoIPStatus{}, err
	}
	ranges, err := loadGeoIPRanges(tmpPath)
	if err != nil {
		return GeoIPStatus{}, err
	}
	if err := os.Rename(tmpPath, g.path); err != nil {
		return GeoIPStatus{}, err
	}
	info, _ := os.Stat(g.path)
	g.mu.Lock()
	g.ranges = ranges
	g.source = source
	if info != nil {
		g.modTime = info.ModTime()
	}
	g.lastErr = ""
	g.mu.Unlock()
	return g.Status(), nil
}

func loadGeoIPRanges(path string) ([]geoIPRange, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return parseGeoIPCSV(gz)
}

func parseGeoIPCSV(r io.Reader) ([]geoIPRange, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	ranges := make([]geoIPRange, 0, 100000)
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) < 3 {
			continue
		}
		start, err := netip.ParseAddr(strings.TrimSpace(row[0]))
		if err != nil {
			continue
		}
		end, err := netip.ParseAddr(strings.TrimSpace(row[1]))
		if err != nil {
			continue
		}
		country := normalizeCountryCode(row[2])
		if country == "" {
			continue
		}
		ranges = append(ranges, geoIPRange{start: start, end: end, country: country})
	}
	if len(ranges) == 0 {
		return nil, errors.New("GeoIP database contains no usable ranges")
	}
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start.Compare(ranges[j].start) < 0
	})
	return ranges, nil
}

func normalizeCountryCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) != 2 {
		return ""
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}
	return value
}

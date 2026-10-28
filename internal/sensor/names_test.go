package sensor_test

import (
	"testing"

	"github.com/pravbeseda/monitor/internal/i18n"
	"github.com/pravbeseda/monitor/internal/sensor"
	"github.com/pravbeseda/monitor/internal/sensor/accesslog"
	"github.com/pravbeseda/monitor/internal/sensor/backup"
	"github.com/pravbeseda/monitor/internal/sensor/disk"
	"github.com/pravbeseda/monitor/internal/sensor/gdrive"
	"github.com/pravbeseda/monitor/internal/sensor/load"
	"github.com/pravbeseda/monitor/internal/sensor/memory"
	"github.com/pravbeseda/monitor/internal/sensor/systemd"
	"github.com/pravbeseda/monitor/internal/sensor/timemachine"
	"github.com/pravbeseda/monitor/internal/sensor/uptime"
)

// spec: metric-names.md#declaration — every built-in sensor names each metric it produces
// and its family, in both languages, as the table says.
func TestBuiltInSensorsNameTheirMetrics(t *testing.T) {
	type name struct{ en, ru string }
	cases := []struct {
		sensor   sensor.Sensor
		families map[string]name
		metrics  map[string]name
	}{
		{disk.New(nil, nil, nil), map[string]name{"disk": {"Disk", "Диск"}}, map[string]name{
			"disk.free_pct":   {"Free on {mount}", "Свободно {mount}"},
			"disk.free_bytes": {"Free on {mount}", "Свободно {mount}"}}},
		{load.New(nil, nil), map[string]name{"load": {"Load", "Нагрузка"}}, map[string]name{
			"load.avg_1m":  {"Load 1 min", "Нагрузка 1 мин"},
			"load.avg_5m":  {"Load 5 min", "Нагрузка 5 мин"},
			"load.avg_15m": {"Load 15 min", "Нагрузка 15 мин"}}},
		{memory.New(nil, nil), map[string]name{"memory": {"Memory", "Память"}}, map[string]name{
			"memory.available_pct":   {"Memory available", "Свободно памяти"},
			"memory.available_bytes": {"Memory available", "Свободно памяти"}}},
		{uptime.New(nil, nil), map[string]name{"uptime": {"Uptime", "Аптайм"}}, map[string]name{
			"uptime.boot_seconds": {"Uptime", "Аптайм"}}},
		{systemd.New(nil, nil), map[string]name{"systemd": {"Services", "Сервисы"}}, map[string]name{
			"systemd.failed_units": {"Failed units", "Упавшие юниты"}}},
		{timemachine.New(nil, nil), map[string]name{"timemachine": {"Time Machine", "Time Machine"}}, map[string]name{
			"timemachine.backup_age_seconds": {"Backup {destination}", "Бэкап {destination}"}}},
		{backup.New("", nil), map[string]name{"backup": {"Backups", "Бэкапы"}}, map[string]name{
			"backup.failed":                        {"Failed {backup} {job}", "Сбой {backup} {job}"},
			"backup.success_age_seconds":           {"Since success {backup} {job}", "С успеха {backup} {job}"},
			"backup.archives":                      {"Archives {backup} {job}", "Архивов {backup} {job}"},
			"backup.latest_archive_age_seconds":    {"Newest archive {backup} {job}", "Свежий архив {backup} {job}"},
			"backup.stored_bytes":                  {"Stored {backup} {job}", "Занято {backup} {job}"},
			"backup.latest_archive_original_bytes": {"Newest archive data {backup} {job}", "Данные архива {backup} {job}"}}},
		{gdrive.New(gdrive.Credentials{}), map[string]name{"gdrive": {"Google Drive", "Google Drive"}}, map[string]name{
			"gdrive.free_pct":   {"Free on Google Drive", "Свободно на Google Drive"},
			"gdrive.free_bytes": {"Free on Google Drive", "Свободно на Google Drive"}}},
		{accesslog.New(nil, nil), map[string]name{"site": {"Site", "Сайт"}}, map[string]name{
			"site.requests_per_min":     {"Requests a minute", "Запросов в минуту"},
			"site.client_error_pct":     {"Client errors", "Ошибки клиента"},
			"site.server_error_pct":     {"Server errors", "Ошибки сервера"},
			"site.response_p95_seconds": {"Response time, p95", "Время ответа, p95"},
			"site.pageviews_24h":        {"Page views, 24 h", "Просмотры за сутки"}}},
	}
	for _, c := range cases {
		t.Run(c.sensor.Name(), func(t *testing.T) {
			named, ok := c.sensor.(sensor.Named)
			if !ok {
				t.Fatal("the sensor names nothing")
			}
			book := named.Names()
			if len(book.Metrics) != len(c.metrics) || len(book.Families) != len(c.families) {
				t.Errorf("names = %v, want exactly %v and %v", book, c.metrics, c.families)
			}
			for id, want := range c.metrics {
				got := book.Metrics[id]
				if got[i18n.English] != want.en || got[i18n.Russian] != want.ru {
					t.Errorf("%s = %v, want %q / %q", id, got, want.en, want.ru)
				}
			}
			for id, want := range c.families {
				got := book.Families[id]
				if got[i18n.English] != want.en || got[i18n.Russian] != want.ru {
					t.Errorf("family %s = %v, want %q / %q", id, got, want.en, want.ru)
				}
			}
		})
	}
}

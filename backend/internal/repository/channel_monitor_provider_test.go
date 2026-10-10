package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 接口接受的官方平台必须能越过 Ent 校验并真正执行 INSERT。
func TestChannelMonitorRepositoryCreateOfficialProvider(t *testing.T) {
	for _, provider := range []string{domain.PlatformCline, domain.PlatformCommandCode} {
		t.Run(provider, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			mock.ExpectQuery(`INSERT INTO "channel_monitors"`).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
			monitor := &service.ChannelMonitor{
				Name: "official-provider-monitor", Provider: provider,
				APIMode: service.MonitorAPIModeChatCompletions, TargetType: service.ChannelMonitorTargetExternal,
				Endpoint: "https://monitor.example.com", APIKey: "encrypted-fixture",
				PrimaryModel: "probe-model", IntervalSeconds: 60, CreatedBy: 1,
			}
			require.NoError(t, NewChannelMonitorRepository(client, db).Create(context.Background(), monitor))
			require.Equal(t, int64(1), monitor.ID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelMonitorTemplateRepositoryCreateOfficialProvider(t *testing.T) {
	for _, provider := range []string{domain.PlatformCline, domain.PlatformCommandCode} {
		t.Run(provider, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			mock.ExpectQuery(`INSERT INTO "channel_monitor_request_templates"`).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
			template := &service.ChannelMonitorRequestTemplate{Name: "official-provider-template", Provider: provider}
			require.NoError(t, NewChannelMonitorRequestTemplateRepository(client, db).Create(context.Background(), template))
			require.Equal(t, int64(1), template.ID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

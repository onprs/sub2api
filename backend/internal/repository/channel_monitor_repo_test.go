package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRepositoryMigrateLocalTargetsRollsBackWholeBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &channelMonitorRepository{db: db}
	targets := []service.ChannelMonitorLocalTargetMigration{
		{MonitorID: 34, GroupID: 20, GroupName: "OpenAI Primary"},
		{MonitorID: 37, GroupID: 21, GroupName: "Anthropic Primary"},
	}

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE channel_monitors").
		WithArgs(int64(20), "OpenAI Primary", int64(34)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE channel_monitors").
		WithArgs(int64(21), "Anthropic Primary", int64(37)).
		WillReturnError(errors.New("write failed"))
	mock.ExpectRollback()

	err = repo.MigrateLocalTargets(context.Background(), targets)
	require.ErrorContains(t, err, "migrate local monitor 37")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorRepositoryMigrateLocalTargetsCommitsCompleteBatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &channelMonitorRepository{db: db}
	targets := []service.ChannelMonitorLocalTargetMigration{
		{MonitorID: 34, GroupID: 20, GroupName: "OpenAI Primary"},
		{MonitorID: 37, GroupID: 21, GroupName: "Anthropic Primary"},
	}

	mock.ExpectBegin()
	for _, target := range targets {
		mock.ExpectExec("UPDATE channel_monitors").
			WithArgs(target.GroupID, target.GroupName, target.MonitorID).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()

	require.NoError(t, repo.MigrateLocalTargets(context.Background(), targets))
	require.NoError(t, mock.ExpectationsWereMet())
}

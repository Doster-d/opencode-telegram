package backend

import (
	"database/sql"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestNewPostgresPairingStore(t *testing.T) {
	t.Run("initializes schema", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		if err != nil {
			t.Fatalf("sqlmock new: %v", err)
		}
		defer db.Close()

		oldOpen := sqlOpen
		sqlOpen = func(driverName, dataSourceName string) (*sql.DB, error) { return db, nil }
		t.Cleanup(func() { sqlOpen = oldOpen })

		mock.ExpectPing()
		mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS oct_pair_codes (")).WillReturnResult(sqlmock.NewResult(0, 0))

		store, err := NewPostgresPairingStore("postgres://x")
		if err != nil {
			t.Fatalf("new store: %v", err)
		}
		if store == nil || store.db == nil {
			t.Fatal("expected initialized store")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("expectations: %v", err)
		}
	})

	t.Run("fails when schema exec fails", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		if err != nil {
			t.Fatalf("sqlmock new: %v", err)
		}
		defer db.Close()

		oldOpen := sqlOpen
		sqlOpen = func(driverName, dataSourceName string) (*sql.DB, error) { return db, nil }
		t.Cleanup(func() { sqlOpen = oldOpen })

		mock.ExpectPing()
		mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS oct_pair_codes (")).WillReturnError(sql.ErrConnDone)

		if _, err := NewPostgresPairingStore("postgres://x"); err == nil {
			t.Fatal("expected schema init error")
		}
	})
}

func TestPostgresPairingStoreMethods(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	store := &PostgresPairingStore{db: db}
	now := time.Now().UTC()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO oct_pair_codes")).WithArgs("PAIR-1", "u1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.SavePairCode("PAIR-1", "u1", now); err != nil {
		t.Fatalf("save pair code: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT telegram_user_id, expires_at FROM oct_pair_codes WHERE code=$1")).WithArgs("PAIR-1").WillReturnRows(sqlmock.NewRows([]string{"telegram_user_id", "expires_at"}).AddRow("u1", now))
	userID, expiresAt, ok, err := store.GetPairCode("PAIR-1")
	if err != nil || !ok || userID != "u1" || expiresAt.IsZero() {
		t.Fatalf("get pair code mismatch user=%q exp=%v ok=%v err=%v", userID, expiresAt, ok, err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT telegram_user_id, expires_at FROM oct_pair_codes WHERE code=$1")).WithArgs("missing").WillReturnError(sql.ErrNoRows)
	_, _, ok, err = store.GetPairCode("missing")
	if err != nil || ok {
		t.Fatalf("expected missing pair code without error, ok=%v err=%v", ok, err)
	}

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM oct_pair_codes WHERE code=$1")).WithArgs("PAIR-1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.DeletePairCode("PAIR-1"); err != nil {
		t.Fatalf("delete pair code: %v", err)
	}

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO oct_agents(telegram_user_id, agent_id, agent_key, updated_at)")).WithArgs("u1", "a1", "k1").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.SaveAgentBinding("u1", "a1", "k1"); err != nil {
		t.Fatalf("save agent binding: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT agent_id FROM oct_agents WHERE agent_key=$1")).WithArgs("k1").WillReturnRows(sqlmock.NewRows([]string{"agent_id"}).AddRow("a1"))
	agentID, ok, err := store.GetAgentIDByKey("k1")
	if err != nil || !ok || agentID != "a1" {
		t.Fatalf("get agent by key mismatch id=%q ok=%v err=%v", agentID, ok, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT agent_id FROM oct_agents WHERE agent_key=$1")).WithArgs("no").WillReturnError(sql.ErrNoRows)
	_, ok, err = store.GetAgentIDByKey("no")
	if err != nil || ok {
		t.Fatalf("expected missing key without error, ok=%v err=%v", ok, err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT agent_id FROM oct_agents WHERE telegram_user_id=$1")).WithArgs("u1").WillReturnRows(sqlmock.NewRows([]string{"agent_id"}).AddRow("a1"))
	agentID, ok, err = store.GetAgentIDByUser("u1")
	if err != nil || !ok || agentID != "a1" {
		t.Fatalf("get agent by user mismatch id=%q ok=%v err=%v", agentID, ok, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT agent_id FROM oct_agents WHERE telegram_user_id=$1")).WithArgs("missing").WillReturnError(sql.ErrNoRows)
	_, ok, err = store.GetAgentIDByUser("missing")
	if err != nil || ok {
		t.Fatalf("expected missing user without error, ok=%v err=%v", ok, err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT telegram_user_id FROM oct_agents WHERE agent_id=$1")).WithArgs("a1").WillReturnRows(sqlmock.NewRows([]string{"telegram_user_id"}).AddRow("u1"))
	uid, ok, err := store.GetUserIDByAgent("a1")
	if err != nil || !ok || uid != "u1" {
		t.Fatalf("get user by agent mismatch uid=%q ok=%v err=%v", uid, ok, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT telegram_user_id FROM oct_agents WHERE agent_id=$1")).WithArgs("missing").WillReturnError(sql.ErrNoRows)
	_, ok, err = store.GetUserIDByAgent("missing")
	if err != nil || ok {
		t.Fatalf("expected missing agent without error, ok=%v err=%v", ok, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

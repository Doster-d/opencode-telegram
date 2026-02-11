package backend

import (
	"database/sql"
	"time"

	_ "github.com/lib/pq"
)

type PostgresPairingStore struct {
	db *sql.DB
}

func NewPostgresPairingStore(dsn string) (*PostgresPairingStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	store := &PostgresPairingStore{db: db}
	if err := store.ensureSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresPairingStore) ensureSchema() error {
	const schema = `
CREATE TABLE IF NOT EXISTS oct_pair_codes (
  code TEXT PRIMARY KEY,
  telegram_user_id TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS oct_agents (
  telegram_user_id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL UNIQUE,
  agent_key TEXT NOT NULL UNIQUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	_, err := s.db.Exec(schema)
	return err
}

func (s *PostgresPairingStore) SavePairCode(code string, telegramUserID string, expiresAt time.Time) error {
	_, err := s.db.Exec(`
INSERT INTO oct_pair_codes(code, telegram_user_id, expires_at)
VALUES($1,$2,$3)
ON CONFLICT (code) DO UPDATE SET telegram_user_id=EXCLUDED.telegram_user_id, expires_at=EXCLUDED.expires_at
`, code, telegramUserID, expiresAt.UTC())
	return err
}

func (s *PostgresPairingStore) GetPairCode(code string) (string, time.Time, bool, error) {
	var userID string
	var expiresAt time.Time
	err := s.db.QueryRow(`SELECT telegram_user_id, expires_at FROM oct_pair_codes WHERE code=$1`, code).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	return userID, expiresAt.UTC(), true, nil
}

func (s *PostgresPairingStore) DeletePairCode(code string) error {
	_, err := s.db.Exec(`DELETE FROM oct_pair_codes WHERE code=$1`, code)
	return err
}

func (s *PostgresPairingStore) SaveAgentBinding(telegramUserID string, agentID string, agentKey string) error {
	_, err := s.db.Exec(`
INSERT INTO oct_agents(telegram_user_id, agent_id, agent_key, updated_at)
VALUES($1,$2,$3,NOW())
ON CONFLICT (telegram_user_id)
DO UPDATE SET agent_id=EXCLUDED.agent_id, agent_key=EXCLUDED.agent_key, updated_at=NOW()
`, telegramUserID, agentID, agentKey)
	return err
}

func (s *PostgresPairingStore) GetAgentIDByKey(agentKey string) (string, bool, error) {
	var agentID string
	err := s.db.QueryRow(`SELECT agent_id FROM oct_agents WHERE agent_key=$1`, agentKey).Scan(&agentID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return agentID, true, nil
}

func (s *PostgresPairingStore) GetAgentIDByUser(telegramUserID string) (string, bool, error) {
	var agentID string
	err := s.db.QueryRow(`SELECT agent_id FROM oct_agents WHERE telegram_user_id=$1`, telegramUserID).Scan(&agentID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return agentID, true, nil
}

func (s *PostgresPairingStore) GetUserIDByAgent(agentID string) (string, bool, error) {
	var userID string
	err := s.db.QueryRow(`SELECT telegram_user_id FROM oct_agents WHERE agent_id=$1`, agentID).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return userID, true, nil
}

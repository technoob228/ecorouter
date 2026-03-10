package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE NOT NULL,
			eco_api_key TEXT UNIQUE NOT NULL,
			deposit_cents_suffix INTEGER UNIQUE NOT NULL,
			or_key_hash TEXT NOT NULL DEFAULT '',
			or_key_secret TEXT NOT NULL DEFAULT '',
			total_deposited_usdt REAL DEFAULT 0.0,
			total_eco_usdt REAL DEFAULT 0.0,
			total_ops_usdt REAL DEFAULT 0.0,
			total_api_credit_usdt REAL DEFAULT 0.0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS deposits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER REFERENCES users(id),
			amount_usdt REAL NOT NULL,
			eco_fee_usdt REAL NOT NULL,
			ops_fee_usdt REAL NOT NULL,
			api_credit_usdt REAL NOT NULL,
			tx_hash TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS donations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			amount_usdt REAL NOT NULL,
			fund_name TEXT NOT NULL,
			fund_category TEXT NOT NULL,
			tx_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS deposit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tx_hash TEXT UNIQUE NOT NULL,
			from_address TEXT NOT NULL,
			amount_usdt REAL NOT NULL,
			user_id INTEGER REFERENCES users(id),
			processed BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

func GenerateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "eco_sk_" + hex.EncodeToString(b), nil
}

// NextCentsSuffix picks a random unique suffix 1-999 for deposit matching.
func (db *DB) NextCentsSuffix() (int, error) {
	for attempts := 0; attempts < 100; attempts++ {
		n, err := rand.Int(rand.Reader, big.NewInt(999))
		if err != nil {
			return 0, err
		}
		suffix := int(n.Int64()) + 1 // 1..999

		var exists bool
		err = db.conn.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE deposit_cents_suffix = ?)", suffix).Scan(&exists)
		if err != nil {
			return 0, err
		}
		if !exists {
			return suffix, nil
		}
	}
	return 0, fmt.Errorf("could not find unique cents suffix after 100 attempts")
}

func (db *DB) CreateUser(email string, centsSuffix int) (*User, error) {
	apiKey, err := GenerateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	result, err := db.conn.Exec(
		"INSERT INTO users (email, eco_api_key, deposit_cents_suffix) VALUES (?, ?, ?)",
		email, apiKey, centsSuffix,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	id, _ := result.LastInsertId()
	return &User{
		ID:                 id,
		Email:              email,
		EcoAPIKey:          apiKey,
		DepositCentsSuffix: centsSuffix,
	}, nil
}

func (db *DB) UpdateUserORKey(userID int64, keyHash, keySecret string) error {
	_, err := db.conn.Exec(
		"UPDATE users SET or_key_hash = ?, or_key_secret = ? WHERE id = ?",
		keyHash, keySecret, userID,
	)
	return err
}

func (db *DB) GetUserByEcoKey(ecoKey string) (*User, error) {
	u := &User{}
	err := db.conn.QueryRow(
		`SELECT id, email, eco_api_key, deposit_cents_suffix, or_key_hash, or_key_secret,
		        total_deposited_usdt, total_eco_usdt, total_ops_usdt, total_api_credit_usdt, created_at
		 FROM users WHERE eco_api_key = ?`, ecoKey,
	).Scan(&u.ID, &u.Email, &u.EcoAPIKey, &u.DepositCentsSuffix, &u.ORKeyHash, &u.ORKeySecret,
		&u.TotalDepositedUSDT, &u.TotalEcoUSDT, &u.TotalOpsUSDT, &u.TotalAPICreditUSDT, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) GetUserByID(id int64) (*User, error) {
	u := &User{}
	err := db.conn.QueryRow(
		`SELECT id, email, eco_api_key, deposit_cents_suffix, or_key_hash, or_key_secret,
		        total_deposited_usdt, total_eco_usdt, total_ops_usdt, total_api_credit_usdt, created_at
		 FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.EcoAPIKey, &u.DepositCentsSuffix, &u.ORKeyHash, &u.ORKeySecret,
		&u.TotalDepositedUSDT, &u.TotalEcoUSDT, &u.TotalOpsUSDT, &u.TotalAPICreditUSDT, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// MatchUserByCents finds user by the fractional cents part of a USDT amount.
// e.g. amount 10.042 → suffix 42, amount 25.007 → suffix 7
func (db *DB) MatchUserByCents(amountUSDT float64) (*User, error) {
	// Extract last 3 decimal digits as cents suffix
	// e.g. 10.042 → 42, 100.123 → 123
	millis := int64(amountUSDT*1000+0.5) % 1000
	suffix := int(millis)
	if suffix == 0 {
		return nil, fmt.Errorf("amount has no cents suffix")
	}

	u := &User{}
	err := db.conn.QueryRow(
		`SELECT id, email, eco_api_key, deposit_cents_suffix, or_key_hash, or_key_secret,
		        total_deposited_usdt, total_eco_usdt, total_ops_usdt, total_api_credit_usdt, created_at
		 FROM users WHERE deposit_cents_suffix = ?`, suffix,
	).Scan(&u.ID, &u.Email, &u.EcoAPIKey, &u.DepositCentsSuffix, &u.ORKeyHash, &u.ORKeySecret,
		&u.TotalDepositedUSDT, &u.TotalEcoUSDT, &u.TotalOpsUSDT, &u.TotalAPICreditUSDT, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) RecordDeposit(userID int64, amount, ecoFee, opsFee, apiCredit float64, txHash string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"INSERT INTO deposits (user_id, amount_usdt, eco_fee_usdt, ops_fee_usdt, api_credit_usdt, tx_hash) VALUES (?, ?, ?, ?, ?, ?)",
		userID, amount, ecoFee, opsFee, apiCredit, txHash,
	)
	if err != nil {
		return fmt.Errorf("insert deposit: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE users SET
			total_deposited_usdt = total_deposited_usdt + ?,
			total_eco_usdt = total_eco_usdt + ?,
			total_ops_usdt = total_ops_usdt + ?,
			total_api_credit_usdt = total_api_credit_usdt + ?
		 WHERE id = ?`,
		amount, ecoFee, opsFee, apiCredit, userID,
	)
	if err != nil {
		return fmt.Errorf("update user totals: %w", err)
	}

	return tx.Commit()
}

func (db *DB) LogDeposit(txHash, fromAddress string, amountUSDT float64, userID *int64) error {
	_, err := db.conn.Exec(
		"INSERT OR IGNORE INTO deposit_log (tx_hash, from_address, amount_usdt, user_id) VALUES (?, ?, ?, ?)",
		txHash, fromAddress, amountUSDT, userID,
	)
	return err
}

func (db *DB) MarkDepositProcessed(txHash string) error {
	_, err := db.conn.Exec("UPDATE deposit_log SET processed = TRUE WHERE tx_hash = ?", txHash)
	return err
}

// ClaimUnmatchedDeposit assigns an unmatched deposit_log entry to the given user.
// Returns the amount if successful, or an error if not found / already claimed.
func (db *DB) ClaimUnmatchedDeposit(txHash string, userID int64) (float64, error) {
	var amount float64
	err := db.conn.QueryRow(
		"SELECT amount_usdt FROM deposit_log WHERE tx_hash = ? AND user_id IS NULL AND processed = FALSE",
		txHash,
	).Scan(&amount)
	if err != nil {
		return 0, fmt.Errorf("deposit not found or already claimed")
	}

	_, err = db.conn.Exec(
		"UPDATE deposit_log SET user_id = ? WHERE tx_hash = ? AND user_id IS NULL AND processed = FALSE",
		userID, txHash,
	)
	if err != nil {
		return 0, fmt.Errorf("claim deposit: %w", err)
	}

	return amount, nil
}

func (db *DB) RecordDonation(amount float64, fundName, fundCategory, txHash string) error {
	_, err := db.conn.Exec(
		"INSERT INTO donations (amount_usdt, fund_name, fund_category, tx_hash) VALUES (?, ?, ?, ?)",
		amount, fundName, fundCategory, txHash,
	)
	return err
}

type Stats struct {
	TotalUsers         int       `json:"total_users"`
	TotalDepositedUSDT float64   `json:"total_deposited_usdt"`
	TotalEcoUSDT       float64   `json:"total_eco_collected_usdt"`
	TotalDonatedUSDT   float64   `json:"total_donated_usdt"`
	Donations          []Donation `json:"donations"`
}

func (db *DB) GetStats() (*Stats, error) {
	s := &Stats{}

	db.conn.QueryRow("SELECT COUNT(*) FROM users").Scan(&s.TotalUsers)
	db.conn.QueryRow("SELECT COALESCE(SUM(total_deposited_usdt), 0) FROM users").Scan(&s.TotalDepositedUSDT)
	db.conn.QueryRow("SELECT COALESCE(SUM(total_eco_usdt), 0) FROM users").Scan(&s.TotalEcoUSDT)
	db.conn.QueryRow("SELECT COALESCE(SUM(amount_usdt), 0) FROM donations").Scan(&s.TotalDonatedUSDT)

	rows, err := db.conn.Query("SELECT id, amount_usdt, fund_name, fund_category, tx_hash, created_at FROM donations ORDER BY created_at DESC LIMIT 50")
	if err != nil {
		return s, nil
	}
	defer rows.Close()

	for rows.Next() {
		var d Donation
		rows.Scan(&d.ID, &d.AmountUSDT, &d.FundName, &d.FundCategory, &d.TxHash, &d.CreatedAt)
		s.Donations = append(s.Donations, d)
	}

	return s, nil
}

func (db *DB) GetUserDepositHistory(userID int64) ([]Deposit, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, amount_usdt, eco_fee_usdt, ops_fee_usdt, api_credit_usdt, tx_hash, created_at
		 FROM deposits WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deposits []Deposit
	for rows.Next() {
		var d Deposit
		if err := rows.Scan(&d.ID, &d.UserID, &d.AmountUSDT, &d.EcoFeeUSDT, &d.OpsFeeUSDT, &d.APICreditUSDT, &d.TxHash, &d.CreatedAt); err != nil {
			return nil, err
		}
		deposits = append(deposits, d)
	}
	return deposits, nil
}

func (db *DB) GetAllActiveUsers() ([]User, error) {
	rows, err := db.conn.Query(
		`SELECT id, email, eco_api_key, deposit_cents_suffix, or_key_hash, or_key_secret,
		        total_deposited_usdt, total_eco_usdt, total_ops_usdt, total_api_credit_usdt, created_at
		 FROM users WHERE or_key_secret != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Email, &u.EcoAPIKey, &u.DepositCentsSuffix, &u.ORKeyHash, &u.ORKeySecret,
			&u.TotalDepositedUSDT, &u.TotalEcoUSDT, &u.TotalOpsUSDT, &u.TotalAPICreditUSDT, &u.CreatedAt)
		users = append(users, u)
	}
	return users, nil
}

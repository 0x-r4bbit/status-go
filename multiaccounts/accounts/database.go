package accounts

import (
	"database/sql"

	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/multiaccounts/errors"
	"github.com/status-im/status-go/multiaccounts/settings"
	"github.com/status-im/status-go/sqlite"
)

const (
	uniqueChatConstraint   = "UNIQUE constraint failed: accounts.chat"
	uniqueWalletConstraint = "UNIQUE constraint failed: accounts.wallet"
)

type Account struct {
	Address   types.Address  `json:"address"`
	Wallet    bool           `json:"wallet"`
	Chat      bool           `json:"chat"`
	Type      string         `json:"type,omitempty"`
	Storage   string         `json:"storage,omitempty"`
	Path      string         `json:"path,omitempty"`
	PublicKey types.HexBytes `json:"public-key,omitempty"`
	Name      string         `json:"name"`
	Color     string         `json:"color"`
	Hidden    bool           `json:"hidden"`
}

const (
	accountTypeGenerated = "generated"
	accountTypeKey       = "key"
	accountTypeSeed      = "seed"
	accountTypeWatch     = "watch"
)

// IsOwnAccount returns true if this is an account we have the private key for
// NOTE: Wallet flag can't be used as it actually indicates that it's the default
// Wallet
func (a *Account) IsOwnAccount() bool {
	return a.Wallet || a.Type == accountTypeSeed || a.Type == accountTypeGenerated || a.Type == accountTypeKey
}

// Database sql wrapper for operations with browser objects.
type Database struct {
	*settings.Database
	db *sql.DB
}

// NewDB returns a new instance of *Database
func NewDB(db *sql.DB) (*Database, error) {
	sDB, err := settings.MakeNewDB(db)
	if err != nil {
		return nil, err
	}

	return &Database{ sDB, db}, nil
}

// DB Gets db sql.DB
func (db Database) DB() *sql.DB {
	return db.db
}

// Close closes database.
func (db Database) Close() error {
	return db.db.Close()
}

func (db *Database) GetAccounts() ([]Account, error) {
	rows, err := db.db.Query("SELECT address, wallet, chat, type, storage, pubkey, path, name, color, hidden FROM accounts ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []Account{}
	pubkey := []byte{}
	for rows.Next() {
		acc := Account{}
		err := rows.Scan(
			&acc.Address, &acc.Wallet, &acc.Chat, &acc.Type, &acc.Storage,
			&pubkey, &acc.Path, &acc.Name, &acc.Color, &acc.Hidden)
		if err != nil {
			return nil, err
		}
		if lth := len(pubkey); lth > 0 {
			acc.PublicKey = make(types.HexBytes, lth)
			copy(acc.PublicKey, pubkey)
		}
		accounts = append(accounts, acc)
	}
	return accounts, nil
}

func (db *Database) GetAccountByAddress(address types.Address) (rst *Account, err error) {
	row := db.db.QueryRow("SELECT address, wallet, chat, type, storage, pubkey, path, name, color, hidden FROM accounts  WHERE address = ? COLLATE NOCASE", address)

	acc := &Account{}
	pubkey := []byte{}
	err = row.Scan(
		&acc.Address, &acc.Wallet, &acc.Chat, &acc.Type, &acc.Storage,
		&pubkey, &acc.Path, &acc.Name, &acc.Color, &acc.Hidden)

	if err != nil {
		return nil, err
	}
	acc.PublicKey = pubkey
	return acc, nil
}

func (db *Database) SaveAccounts(accounts []Account) (err error) {
	var (
		tx     *sql.Tx
		insert *sql.Stmt
		update *sql.Stmt
	)
	tx, err = db.db.Begin()
	if err != nil {
		return
	}
	defer func() {
		if err == nil {
			err = tx.Commit()
			return
		}
		_ = tx.Rollback()
	}()
	// NOTE(dshulyak) replace all record values using address (primary key)
	// can't use `insert or replace` because of the additional constraints (wallet and chat)
	insert, err = tx.Prepare("INSERT OR IGNORE INTO accounts (address, created_at, updated_at) VALUES (?, datetime('now'), datetime('now'))")
	if err != nil {
		return err
	}
	update, err = tx.Prepare("UPDATE accounts SET wallet = ?, chat = ?, type = ?, storage = ?, pubkey = ?, path = ?, name = ?, color = ?, hidden = ?, updated_at = datetime('now') WHERE address = ?")
	if err != nil {
		return err
	}
	for i := range accounts {
		acc := &accounts[i]
		_, err = insert.Exec(acc.Address)
		if err != nil {
			return
		}
		_, err = update.Exec(acc.Wallet, acc.Chat, acc.Type, acc.Storage, acc.PublicKey, acc.Path, acc.Name, acc.Color, acc.Hidden, acc.Address)
		if err != nil {
			switch err.Error() {
			case uniqueChatConstraint:
				err = errors.ErrChatNotUnique
			case uniqueWalletConstraint:
				err = errors.ErrWalletNotUnique
			}
			return
		}
	}
	return
}

func (db *Database) DeleteAccount(address types.Address) error {
	_, err := db.db.Exec("DELETE FROM accounts WHERE address = ?", address)
	return err
}

func (db *Database) DeleteSeedAndKeyAccounts() error {
	_, err := db.db.Exec("DELETE FROM accounts WHERE type = ? OR type = ?", accountTypeSeed, accountTypeKey)
	return err
}

func (db *Database) GetNotificationsEnabled() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT notifications_enabled FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

func (db *Database) GetProfilePicturesVisibility() (int, error) {
	var result int
	err := db.db.QueryRow("SELECT profile_pictures_visibility FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

func (db *Database) GetPublicKey() (rst string, err error) {
	err = db.db.QueryRow("SELECT public_key FROM settings WHERE synthetic_id = 'id'").Scan(&rst)
	if err == sql.ErrNoRows {
		return rst, nil
	}
	return
}

func (db *Database) GetFleet() (rst string, err error) {
	err = db.db.QueryRow("SELECT COALESCE(fleet, '') FROM settings WHERE synthetic_id = 'id'").Scan(&rst)
	if err == sql.ErrNoRows {
		return rst, nil
	}
	return
}

func (db *Database) GetDappsAddress() (rst types.Address, err error) {
	err = db.db.QueryRow("SELECT dapps_address FROM settings WHERE synthetic_id = 'id'").Scan(&rst)
	if err == sql.ErrNoRows {
		return rst, nil
	}
	return
}

func (db *Database) CanUseMailservers() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT use_mailservers FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

func (db *Database) CanSyncOnMobileNetwork() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT syncing_on_mobile_network FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

func (db *Database) GetDefaultSyncPeriod() (uint32, error) {
	var result uint32
	err := db.db.QueryRow("SELECT default_sync_period FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

func (db *Database) GetMessagesFromContactsOnly() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT messages_from_contacts_only FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

func (db *Database) GetWalletAddress() (rst types.Address, err error) {
	err = db.db.QueryRow("SELECT address FROM accounts WHERE wallet = 1").Scan(&rst)
	return
}

func (db *Database) GetWalletAddresses() (rst []types.Address, err error) {
	rows, err := db.db.Query("SELECT address FROM accounts WHERE chat = 0 ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		addr := types.Address{}
		err = rows.Scan(&addr)
		if err != nil {
			return nil, err
		}
		rst = append(rst, addr)
	}
	return rst, nil
}

func (db *Database) GetChatAddress() (rst types.Address, err error) {
	err = db.db.QueryRow("SELECT address FROM accounts WHERE chat = 1").Scan(&rst)
	return
}

func (db *Database) GetAddresses() (rst []types.Address, err error) {
	rows, err := db.db.Query("SELECT address FROM accounts ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		addr := types.Address{}
		err = rows.Scan(&addr)
		if err != nil {
			return nil, err
		}
		rst = append(rst, addr)
	}
	return rst, nil
}

// AddressExists returns true if given address is stored in database.
func (db *Database) AddressExists(address types.Address) (exists bool, err error) {
	err = db.db.QueryRow("SELECT EXISTS (SELECT 1 FROM accounts WHERE address = ?)", address).Scan(&exists)
	return exists, err
}

func (db *Database) GetCurrentStatus(status interface{}) error {
	err := db.db.QueryRow("SELECT current_user_status FROM settings WHERE synthetic_id = 'id'").Scan(&sqlite.JSONBlob{Data: &status})
	if err == sql.ErrNoRows {
		return nil
	}
	return err
}

func (db *Database) ShouldBroadcastUserStatus() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT send_status_updates FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	// If the `send_status_updates` value is nil the sql.ErrNoRows will be returned
	// because this feature is opt out, `true` should be returned in the case where no value is found
	if err == sql.ErrNoRows {
		return true, nil
	}
	return result, err
}

func (db *Database) BackupEnabled() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT backup_enabled FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return result, err
}

func (db *Database) AutoMessageEnabled() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT auto_message_enabled FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return result, err
}

func (db *Database) LastBackup() (uint64, error) {
	var result uint64
	err := db.db.QueryRow("SELECT last_backup FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return result, err
}

func (db *Database) SetLastBackup(time uint64) error {
	_, err := db.db.Exec("UPDATE settings SET last_backup = ?", time)
	return err
}

func (db *Database) SetBackupFetched(fetched bool) error {
	_, err := db.db.Exec("UPDATE settings SET backup_fetched = ?", fetched)
	return err
}

func (db *Database) BackupFetched() (bool, error) {
	var result bool
	err := db.db.QueryRow("SELECT backup_fetched FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return true, nil
	}
	return result, err
}

func (db *Database) ENSName() (string, error) {
	var result sql.NullString
	err := db.db.QueryRow("SELECT preferred_name FROM settings WHERE synthetic_id = 'id'").Scan(&result)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if result.Valid {
		return result.String, nil
	}
	return "", err
}

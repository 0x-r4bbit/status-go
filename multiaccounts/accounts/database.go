package accounts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/nodecfg"
	"github.com/status-im/status-go/params"
	"github.com/status-im/status-go/sqlite"
)

const (
	uniqueChatConstraint   = "UNIQUE constraint failed: accounts.chat"
	uniqueWalletConstraint = "UNIQUE constraint failed: accounts.wallet"
)

type ProfilePicturesVisibilityType int

const (
	ProfilePicturesVisibilityContactsOnly ProfilePicturesVisibilityType = iota + 1
	ProfilePicturesVisibilityEveryone
	ProfilePicturesVisibilityNone
)

type ProfilePicturesShowToType int

const (
	ProfilePicturesShowToContactsOnly ProfilePicturesShowToType = iota + 1
	ProfilePicturesShowToEveryone
	ProfilePicturesShowToNone
)

var (
	// ErrWalletNotUnique returned if another account has `wallet` field set to true.
	ErrWalletNotUnique = errors.New("another account is set to be default wallet. disable it before using new")
	// ErrChatNotUnique returned if another account has `chat` field set to true.
	ErrChatNotUnique = errors.New("another account is set to be default chat. disable it before using new")
	// ErrInvalidConfig returned if config isn't allowed
	ErrInvalidConfig = errors.New("configuration value not allowed")
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

type Settings struct {
	// required
	Address                   types.Address    `json:"address"`
	AnonMetricsShouldSend     bool             `json:"anon-metrics/should-send?,omitempty"`
	ChaosMode                 bool             `json:"chaos-mode?,omitempty"`
	Currency                  string           `json:"currency,omitempty"`
	CurrentNetwork            string           `json:"networks/current-network"`
	CustomBootnodes           *json.RawMessage `json:"custom-bootnodes,omitempty"`
	CustomBootnodesEnabled    *json.RawMessage `json:"custom-bootnodes-enabled?,omitempty"`
	DappsAddress              types.Address    `json:"dapps-address"`
	EIP1581Address            types.Address    `json:"eip1581-address"`
	Fleet                     *string          `json:"fleet,omitempty"`
	HideHomeTooltip           bool             `json:"hide-home-tooltip?,omitempty"`
	InstallationID            string           `json:"installation-id"`
	KeyUID                    string           `json:"key-uid"`
	KeycardInstanceUID        string           `json:"keycard-instance-uid,omitempty"`
	KeycardPAiredOn           int64            `json:"keycard-paired-on,omitempty"`
	KeycardPairing            string           `json:"keycard-pairing,omitempty"`
	LastUpdated               *int64           `json:"last-updated,omitempty"`
	LatestDerivedPath         uint             `json:"latest-derived-path"`
	LinkPreviewRequestEnabled bool             `json:"link-preview-request-enabled,omitempty"`
	LinkPreviewsEnabledSites  *json.RawMessage `json:"link-previews-enabled-sites,omitempty"`
	LogLevel                  *string          `json:"log-level,omitempty"`
	MessagesFromContactsOnly  bool             `json:"messages-from-contacts-only"`
	Mnemonic                  *string          `json:"mnemonic,omitempty"`
	Name                      string           `json:"name,omitempty"`
	Networks                  *json.RawMessage `json:"networks/networks"`
	// NotificationsEnabled indicates whether local notifications should be enabled (android only)
	NotificationsEnabled bool             `json:"notifications-enabled?,omitempty"`
	PhotoPath            string           `json:"photo-path"`
	PinnedMailserver     *json.RawMessage `json:"pinned-mailservers,omitempty"`
	PreferredName        *string          `json:"preferred-name,omitempty"`
	PreviewPrivacy       bool             `json:"preview-privacy?"`
	PublicKey            string           `json:"public-key"`
	// PushNotificationsServerEnabled indicates whether we should be running a push notification server
	PushNotificationsServerEnabled bool `json:"push-notifications-server-enabled?,omitempty"`
	// PushNotificationsFromContactsOnly indicates whether we should only receive push notifications from contacts
	PushNotificationsFromContactsOnly bool `json:"push-notifications-from-contacts-only?,omitempty"`
	// PushNotificationsBlockMentions indicates whether we should receive notifications for mentions
	PushNotificationsBlockMentions bool `json:"push-notifications-block-mentions?,omitempty"`
	RememberSyncingChoice          bool `json:"remember-syncing-choice?,omitempty"`
	// RemotePushNotificationsEnabled indicates whether we should be using remote notifications (ios only for now)
	RemotePushNotificationsEnabled bool             `json:"remote-push-notifications-enabled?,omitempty"`
	SigningPhrase                  string           `json:"signing-phrase"`
	StickerPacksInstalled          *json.RawMessage `json:"stickers/packs-installed,omitempty"`
	StickerPacksPending            *json.RawMessage `json:"stickers/packs-pending,omitempty"`
	StickersRecentStickers         *json.RawMessage `json:"stickers/recent-stickers,omitempty"`
	SyncingOnMobileNetwork         bool             `json:"syncing-on-mobile-network?,omitempty"`
	// DefaultSyncPeriod is how far back in seconds we should pull messages from a mailserver
	DefaultSyncPeriod uint `json:"default-sync-period"`
	// SendPushNotifications indicates whether we should send push notifications for other clients
	SendPushNotifications bool `json:"send-push-notifications?,omitempty"`
	Appearance            uint `json:"appearance"`
	// ProfilePicturesShowTo indicates to whom the user shows their profile picture to (contacts, everyone)
	ProfilePicturesShowTo ProfilePicturesShowToType `json:"profile-pictures-show-to"`
	// ProfilePicturesVisibility indicates who we want to see profile pictures of (contacts, everyone or none)
	ProfilePicturesVisibility      ProfilePicturesVisibilityType `json:"profile-pictures-visibility"`
	UseMailservers                 bool                          `json:"use-mailservers?"`
	Usernames                      *json.RawMessage              `json:"usernames,omitempty"`
	WalletRootAddress              types.Address                 `json:"wallet-root-address,omitempty"`
	WalletSetUpPassed              bool                          `json:"wallet-set-up-passed?,omitempty"`
	WalletVisibleTokens            *json.RawMessage              `json:"wallet/visible-tokens,omitempty"`
	WakuBloomFilterMode            bool                          `json:"waku-bloom-filter-mode,omitempty"`
	WebViewAllowPermissionRequests bool                          `json:"webview-allow-permission-requests?,omitempty"`
	SendStatusUpdates              bool                          `json:"send-status-updates?,omitempty"`
	CurrentUserStatus              *json.RawMessage              `json:"current-user-status"`
	GifRecents                     *json.RawMessage              `json:"gifs/recent-gifs"`
	GifFavorites                   *json.RawMessage              `json:"gifs/favorite-gifs"`
	OpenseaEnabled                 bool                          `json:"opensea-enabled?,omitempty"`
	TelemetryServerURL             string                        `json:"telemetry-server-url,omitempty"`
	LastBackup                     uint64                        `json:"last-backup,omitempty"`
	BackupEnabled                  bool                          `json:"backup-enabled?,omitempty"`
	AutoMessageEnabled             bool                          `json:"auto-message-enabled?,omitempty"`
}

func NewDB(db *sql.DB) *Database {
	return &Database{db: db}
}

// Database sql wrapper for operations with browser objects.
type Database struct {
	db *sql.DB
}

// Get database
func (db Database) DB() *sql.DB {
	return db.db
}

// Close closes database.
func (db Database) Close() error {
	return db.db.Close()
}

// TODO remove photoPath from settings
func (db *Database) CreateSettings(s Settings, n params.NodeConfig) error {
	tx, err := db.db.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return err
	}

	defer func() {
		if err == nil {
			err = tx.Commit()
			return
		}
		// don't shadow original error
		_ = tx.Rollback()
	}()

	_, err = tx.Exec(`
INSERT INTO settings (
  address,
  currency,
  current_network,
  dapps_address,
  eip1581_address,
  installation_id,
  key_uid,
  keycard_instance_uid,
  keycard_paired_on,
  keycard_pairing,
  latest_derived_path,
  mnemonic,
  name,
  networks,
  photo_path,
  preview_privacy,
  public_key,
  signing_phrase,
  wallet_root_address,
  synthetic_id
) VALUES (
?,?,?,?,?,?,?,?,?,?,
?,?,?,?,?,?,?,?,?,'id')`,
		s.Address,
		s.Currency,
		s.CurrentNetwork,
		s.DappsAddress,
		s.EIP1581Address,
		s.InstallationID,
		s.KeyUID,
		s.KeycardInstanceUID,
		s.KeycardPAiredOn,
		s.KeycardPairing,
		s.LatestDerivedPath,
		s.Mnemonic,
		s.Name,
		s.Networks,
		s.PhotoPath,
		s.PreviewPrivacy,
		s.PublicKey,
		s.SigningPhrase,
		s.WalletRootAddress,
	)
	if err != nil {
		return err
	}

	return nodecfg.SaveConfigWithTx(tx, &n)
}

func (db *Database) SaveSetting(setting string, value interface{}) error {
	var (
		update *sql.Stmt
		err    error
	)

	switch setting {
	case "chaos-mode?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET chaos_mode = ? WHERE synthetic_id = 'id'")
	case "currency":
		update, err = db.db.Prepare("UPDATE settings SET currency = ? WHERE synthetic_id = 'id'")
	case "custom-bootnodes":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET custom_bootnodes = ? WHERE synthetic_id = 'id'")
	case "custom-bootnodes-enabled?":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET custom_bootnodes_enabled = ? WHERE synthetic_id = 'id'")
	case "dapps-address":
		str, ok := value.(string)
		if ok {
			value = types.HexToAddress(str)
		} else {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET dapps_address = ? WHERE synthetic_id = 'id'")
	case "eip1581-address":
		str, ok := value.(string)
		if ok {
			value = types.HexToAddress(str)
		} else {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET eip1581_address = ? WHERE synthetic_id = 'id'")
	case "fleet":
		update, err = db.db.Prepare("UPDATE settings SET fleet = ? WHERE synthetic_id = 'id'")
	case "hide-home-tooltip?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET hide_home_tooltip = ? WHERE synthetic_id = 'id'")
	case "messages-from-contacts-only":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET messages_from_contacts_only = ? WHERE synthetic_id = 'id'")
	case "keycard-instance_uid":
		update, err = db.db.Prepare("UPDATE settings SET keycard_instance_uid = ? WHERE synthetic_id = 'id'")
	case "keycard-paired_on":
		update, err = db.db.Prepare("UPDATE settings SET keycard_paired_on = ? WHERE synthetic_id = 'id'")
	case "keycard-pairing":
		update, err = db.db.Prepare("UPDATE settings SET keycard_pairing = ? WHERE synthetic_id = 'id'")
	case "last-updated":
		update, err = db.db.Prepare("UPDATE settings SET last_updated = ? WHERE synthetic_id = 'id'")
	case "latest-derived-path":
		update, err = db.db.Prepare("UPDATE settings SET latest_derived_path = ? WHERE synthetic_id = 'id'")
	case "link-preview-request-enabled":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET link_preview_request_enabled = ? WHERE synthetic_id = 'id'")
	case "link-previews-enabled-sites":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET link_previews_enabled_sites = ? WHERE synthetic_id = 'id'")
	case "log-level":
		update, err = db.db.Prepare("UPDATE settings SET log_level = ? WHERE synthetic_id = 'id'")
	case "mnemonic":
		update, err = db.db.Prepare("UPDATE settings SET mnemonic = ? WHERE synthetic_id = 'id'")
	case "name":
		update, err = db.db.Prepare("UPDATE settings SET name = ? WHERE synthetic_id = 'id'")
	case "networks/current-network":
		update, err = db.db.Prepare("UPDATE settings SET current_network = ? WHERE synthetic_id = 'id'")
	case "networks/networks":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET networks = ? WHERE synthetic_id = 'id'")
	case "node-config":
		var jsonString []byte
		jsonString, err = json.Marshal(value)
		if err != nil {
			return err
		}
		var nodeConfig params.NodeConfig
		err = json.Unmarshal(jsonString, &nodeConfig)
		if err != nil {
			return err
		}
		if err = nodecfg.SaveNodeConfig(db.db, &nodeConfig); err != nil {
			return err
		}
		value = nil
		update, err = db.db.Prepare("UPDATE settings SET node_config = ? WHERE synthetic_id = 'id'")
	case "notifications-enabled?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET notifications_enabled = ? WHERE synthetic_id = 'id'")
	case "photo-path":
		update, err = db.db.Prepare("UPDATE settings SET photo_path = ? WHERE synthetic_id = 'id'")
	case "pinned-mailservers":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET pinned_mailservers = ? WHERE synthetic_id = 'id'")
	case "preferred-name":
		update, err = db.db.Prepare("UPDATE settings SET preferred_name = ? WHERE synthetic_id = 'id'")
	case "preview-privacy?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET preview_privacy = ? WHERE synthetic_id = 'id'")
	case "public-key":
		update, err = db.db.Prepare("UPDATE settings SET public_key = ? WHERE synthetic_id = 'id'")
	case "remember-syncing-choice?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET remember_syncing_choice = ? WHERE synthetic_id = 'id'")
	case "remote-push-notifications-enabled?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET remote_push_notifications_enabled = ? WHERE synthetic_id = 'id'")
	case "push-notifications-server-enabled?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET push_notifications_server_enabled = ? WHERE synthetic_id = 'id'")
	case "push-notifications-from-contacts-only?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET push_notifications_from_contacts_only = ? WHERE synthetic_id = 'id'")
	case "push-notifications-block-mentions?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET push_notifications_block_mentions = ? WHERE synthetic_id = 'id'")
	case "send-push-notifications?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET send_push_notifications = ? WHERE synthetic_id = 'id'")
	case "stickers/packs-installed":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET stickers_packs_installed = ? WHERE synthetic_id = 'id'")
	case "stickers/packs-pending":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET stickers_packs_pending = ? WHERE synthetic_id = 'id'")
	case "stickers/recent-stickers":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET stickers_recent_stickers = ? WHERE synthetic_id = 'id'")
	case "syncing-on-mobile-network?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET syncing_on_mobile_network = ? WHERE synthetic_id = 'id'")
	case "use-mailservers?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET use_mailservers = ? WHERE synthetic_id = 'id'")
	case "default-sync-period":
		update, err = db.db.Prepare("UPDATE settings SET default_sync_period = ? WHERE synthetic_id = 'id'")
	case "usernames":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET usernames = ? WHERE synthetic_id = 'id'")
	case "wallet-set-up-passed?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET wallet_set_up_passed = ? WHERE synthetic_id = 'id'")
	case "wallet/visible-tokens":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET wallet_visible_tokens = ? WHERE synthetic_id = 'id'")
	case "appearance":
		update, err = db.db.Prepare("UPDATE settings SET appearance = ? WHERE synthetic_id = 'id'")
	case "profile-pictures-show-to":
		update, err = db.db.Prepare("UPDATE settings SET profile_pictures_show_to = ? WHERE synthetic_id = 'id'")
	case "profile-pictures-visibility":
		update, err = db.db.Prepare("UPDATE settings SET profile_pictures_visibility = ? WHERE synthetic_id = 'id'")
	case "waku-bloom-filter-mode":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET waku_bloom_filter_mode = ? WHERE synthetic_id = 'id'")
	case "webview-allow-permission-requests?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET webview_allow_permission_requests = ? WHERE synthetic_id = 'id'")
	case "anon-metrics/should-send?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET anon_metrics_should_send = ? WHERE synthetic_id = 'id'")
	case "current-user-status":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET current_user_status = ? WHERE synthetic_id = 'id'")
	case "send-status-updates?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET send_status_updates = ? WHERE synthetic_id = 'id'")
	case "gifs/recent-gifs":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET gif_recents = ? WHERE synthetic_id = 'id'")
	case "gifs/favorite-gifs":
		value = &sqlite.JSONBlob{Data: value}
		update, err = db.db.Prepare("UPDATE settings SET gif_favorites = ? WHERE synthetic_id = 'id'")
	case "opensea-enabled?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET opensea_enabled = ? WHERE synthetic_id = 'id'")
	case "telemetry-server-url":
		update, err = db.db.Prepare("UPDATE settings SET telemetry_server_url = ? WHERE synthetic_id = 'id'")
	case "backup-enabled?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}
		update, err = db.db.Prepare("UPDATE settings SET backup_enabled = ? WHERE synthetic_id = 'id'")
	case "auto-message-enabled?":
		_, ok := value.(bool)
		if !ok {
			return ErrInvalidConfig
		}

		update, err = db.db.Prepare("UPDATE settings SET auto_message_enabled = ? WHERE synthetic_id = 'id'")

	default:
		return ErrInvalidConfig
	}
	if err != nil {
		return err
	}

	_, err = update.Exec(value)
	return err
}

func (db *Database) GetSettings() (Settings, error) {
	var s Settings
	err := db.db.QueryRow("SELECT address, anon_metrics_should_send, chaos_mode, currency, current_network, custom_bootnodes, custom_bootnodes_enabled, dapps_address, eip1581_address, fleet, hide_home_tooltip, installation_id, key_uid, keycard_instance_uid, keycard_paired_on, keycard_pairing, last_updated, latest_derived_path, link_preview_request_enabled, link_previews_enabled_sites, log_level, mnemonic, name, networks, notifications_enabled, push_notifications_server_enabled, push_notifications_from_contacts_only, remote_push_notifications_enabled, send_push_notifications, push_notifications_block_mentions, photo_path, pinned_mailservers, preferred_name, preview_privacy, public_key, remember_syncing_choice, signing_phrase, stickers_packs_installed, stickers_packs_pending, stickers_recent_stickers, syncing_on_mobile_network, default_sync_period, use_mailservers, messages_from_contacts_only, usernames, appearance, profile_pictures_show_to, profile_pictures_visibility, wallet_root_address, wallet_set_up_passed, wallet_visible_tokens, waku_bloom_filter_mode, webview_allow_permission_requests, current_user_status, send_status_updates, gif_recents, gif_favorites, opensea_enabled, last_backup, backup_enabled, telemetry_server_url, auto_message_enabled FROM settings WHERE synthetic_id = 'id'").Scan(
		&s.Address,
		&s.AnonMetricsShouldSend,
		&s.ChaosMode,
		&s.Currency,
		&s.CurrentNetwork,
		&s.CustomBootnodes,
		&s.CustomBootnodesEnabled,
		&s.DappsAddress,
		&s.EIP1581Address,
		&s.Fleet,
		&s.HideHomeTooltip,
		&s.InstallationID,
		&s.KeyUID,
		&s.KeycardInstanceUID,
		&s.KeycardPAiredOn,
		&s.KeycardPairing,
		&s.LastUpdated,
		&s.LatestDerivedPath,
		&s.LinkPreviewRequestEnabled,
		&s.LinkPreviewsEnabledSites,
		&s.LogLevel,
		&s.Mnemonic,
		&s.Name,
		&s.Networks,
		&s.NotificationsEnabled,
		&s.PushNotificationsServerEnabled,
		&s.PushNotificationsFromContactsOnly,
		&s.RemotePushNotificationsEnabled,
		&s.SendPushNotifications,
		&s.PushNotificationsBlockMentions,
		&s.PhotoPath,
		&s.PinnedMailserver,
		&s.PreferredName,
		&s.PreviewPrivacy,
		&s.PublicKey,
		&s.RememberSyncingChoice,
		&s.SigningPhrase,
		&s.StickerPacksInstalled,
		&s.StickerPacksPending,
		&s.StickersRecentStickers,
		&s.SyncingOnMobileNetwork,
		&s.DefaultSyncPeriod,
		&s.UseMailservers,
		&s.MessagesFromContactsOnly,
		&s.Usernames,
		&s.Appearance,
		&s.ProfilePicturesShowTo,
		&s.ProfilePicturesVisibility,
		&s.WalletRootAddress,
		&s.WalletSetUpPassed,
		&s.WalletVisibleTokens,
		&s.WakuBloomFilterMode,
		&s.WebViewAllowPermissionRequests,
		&sqlite.JSONBlob{Data: &s.CurrentUserStatus},
		&s.SendStatusUpdates,
		&sqlite.JSONBlob{Data: &s.GifRecents},
		&sqlite.JSONBlob{Data: &s.GifFavorites},
		&s.OpenseaEnabled,
		&s.LastBackup,
		&s.BackupEnabled,
		&s.TelemetryServerURL,
		&s.AutoMessageEnabled,
	)

	return s, err
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
				err = ErrChatNotUnique
			case uniqueWalletConstraint:
				err = ErrWalletNotUnique
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

func (db *Database) GetPinnedMailservers() (rst map[string]string, err error) {
	rst = make(map[string]string)
	var pinnedMailservers string
	err = db.db.QueryRow("SELECT COALESCE(pinned_mailservers, '') FROM settings WHERE synthetic_id = 'id'").Scan(&pinnedMailservers)
	if err == sql.ErrNoRows || pinnedMailservers == "" {
		return rst, nil
	}

	err = json.Unmarshal([]byte(pinnedMailservers), &rst)
	if err != nil {
		return nil, err
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

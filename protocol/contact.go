package protocol

import (
	"crypto/ecdsa"

	"github.com/status-im/status-go/eth-node/crypto"
	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/images"
	"github.com/status-im/status-go/multiaccounts/accounts"
	"github.com/status-im/status-go/protocol/identity/alias"
	"github.com/status-im/status-go/protocol/identity/identicon"
)

// ContactDeviceInfo is a struct containing information about a particular device owned by a contact
type ContactDeviceInfo struct {
	// The installation id of the device
	InstallationID string `json:"id"`
	// Timestamp represents the last time we received this info
	Timestamp int64 `json:"timestamp"`
	// FCMToken is to be used for push notifications
	FCMToken string `json:"fcmToken"`
}

func (c *Contact) CanonicalName() string {
	if c.LocalNickname != "" {
		return c.LocalNickname
	}

	if c.ENSVerified {
		return c.Name
	}

	return c.Alias
}

func (c *Contact) CanonicalImage(profilePicturesVisibility accounts.ProfilePicturesVisibilityType) string {
	if profilePicturesVisibility == accounts.ProfilePicturesVisibilityNone || (profilePicturesVisibility == accounts.ProfilePicturesVisibilityContactsOnly && !c.Added) {
		return c.Identicon
	}

	if largeImage, ok := c.Images[images.LargeDimName]; ok {
		imageBase64, err := largeImage.GetDataURI()
		if err == nil {
			return imageBase64
		}
	}

	if thumbImage, ok := c.Images[images.SmallDimName]; ok {
		imageBase64, err := thumbImage.GetDataURI()
		if err == nil {
			return imageBase64
		}
	}

	return c.Identicon
}

// Contact has information about a "Contact"
type Contact struct {
	// ID of the contact. It's a hex-encoded public key (prefixed with 0x).
	ID string `json:"id"`
	// Ethereum address of the contact
	Address string `json:"address,omitempty"`
	// ENS name of contact
	Name string `json:"name,omitempty"`
	// EnsVerified whether we verified the name of the contact
	ENSVerified bool `json:"ensVerified"`
	// Generated username name of the contact
	Alias string `json:"alias,omitempty"`
	// Identicon generated from public key
	Identicon string `json:"identicon"`
	// LastUpdated is the last time we received an update from the contact
	// updates should be discarded if last updated is less than the one stored
	LastUpdated uint64 `json:"lastUpdated"`

	// LastUpdatedLocally is the last time we updated the contact locally
	LastUpdatedLocally uint64 `json:"lastUpdatedLocally"`

	LocalNickname string `json:"localNickname,omitempty"`

	Images map[string]images.IdentityImage `json:"images"`

	Added      bool `json:"added"`
	Blocked    bool `json:"blocked"`
	HasAddedUs bool `json:"hasAddedUs"`

	IsSyncing bool
	Removed   bool
}

func (c Contact) PublicKey() (*ecdsa.PublicKey, error) {
	b, err := types.DecodeHex(c.ID)
	if err != nil {
		return nil, err
	}
	return crypto.UnmarshalPubkey(b)
}

func (c *Contact) Block() {
	c.Blocked = true
	c.Added = false
}

func (c *Contact) Unblock() {
	c.Blocked = false
}

func (c *Contact) Remove() {
	c.Added = false
	c.Removed = true
}

func buildContactFromPkString(pkString string) (*Contact, error) {
	publicKeyBytes, err := types.DecodeHex(pkString)
	if err != nil {
		return nil, err
	}

	publicKey, err := crypto.UnmarshalPubkey(publicKeyBytes)
	if err != nil {
		return nil, err
	}

	return buildContact(pkString, publicKey)
}

func BuildContactFromPublicKey(publicKey *ecdsa.PublicKey) (*Contact, error) {
	id := types.EncodeHex(crypto.FromECDSAPub(publicKey))
	return buildContact(id, publicKey)
}

func buildContact(publicKeyString string, publicKey *ecdsa.PublicKey) (*Contact, error) {
	newIdenticon, err := identicon.GenerateBase64(publicKeyString)
	if err != nil {
		return nil, err
	}

	contact := &Contact{
		ID:        publicKeyString,
		Alias:     alias.GenerateFromPublicKey(publicKey),
		Identicon: newIdenticon,
	}

	return contact, nil
}

func contactIDFromPublicKey(key *ecdsa.PublicKey) string {
	return types.EncodeHex(crypto.FromECDSAPub(key))
}

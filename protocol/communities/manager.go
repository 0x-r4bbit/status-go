package communities

import (
	"crypto/ecdsa"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"os"
	"log"

  "github.com/anacrolix/torrent"
  "github.com/anacrolix/torrent/bencode"
  "github.com/anacrolix/torrent/metainfo"
	"github.com/golang/protobuf/proto"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/status-im/status-go/eth-node/crypto"
	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/protocol/common"
	"github.com/status-im/status-go/protocol/ens"
	"github.com/status-im/status-go/params"
	"github.com/status-im/status-go/protocol/protobuf"
	"github.com/status-im/status-go/protocol/transport"
	"github.com/status-im/status-go/protocol/requests"
)

var defaultAnnounceList = [][]string{
    {"http://p4p.arenabg.com:1337/announce"},
    {"udp://tracker.opentrackr.org:1337/announce"},
    {"udp://tracker.openbittorrent.com:6969/announce"},
}
var pieceLength = 1024

type Manager struct {
	persistence     *Persistence
	ensSubscription chan []*ens.VerificationRecord
	subscriptions   []chan *Subscription
	ensVerifier     *ens.Verifier
	identity        *ecdsa.PublicKey
	logger          *zap.Logger
  transport       *transport.Transport
	quit            chan struct{}
  torrentConfig params.TorrentConfig
  torrentClient *torrent.Client
  messageArchiveCreationTasks   map[string]chan struct{}
  torrentTasks map[string]metainfo.Hash
}

func NewManager(identity *ecdsa.PublicKey, db *sql.DB, logger *zap.Logger, verifier *ens.Verifier, transport *transport.Transport, torrentConfig params.TorrentConfig) (*Manager, error) {
	if identity == nil {
		return nil, errors.New("empty identity")
	}

	var err error
	if logger == nil {
		if logger, err = zap.NewDevelopment(); err != nil {
			return nil, errors.Wrap(err, "failed to create a logger")
		}
	}

	manager := &Manager{
		logger:   logger,
		identity: identity,
		quit:     make(chan struct{}),
    transport: transport,
    torrentConfig: torrentConfig,
    messageArchiveCreationTasks: make(map[string]chan struct{}),
    torrentTasks: make(map[string]metainfo.Hash),
		persistence: &Persistence{
			logger: logger,
			db:     db,
		},
	}

	if verifier != nil {

		sub := verifier.Subscribe()
		manager.ensSubscription = sub
		manager.ensVerifier = verifier
	}

	return manager, nil
}

type Subscription struct {
	Community   *Community
	Invitations []*protobuf.CommunityInvitation
}

type CommunityResponse struct {
	Community *Community        `json:"community"`
	Changes   *CommunityChanges `json:"changes"`
}

func (m *Manager) Subscribe() chan *Subscription {
	subscription := make(chan *Subscription, 100)
	m.subscriptions = append(m.subscriptions, subscription)
	return subscription
}

func (m *Manager) Start() error {
	if m.ensVerifier != nil {
		m.runENSVerificationLoop()
	}

  if m.torrentConfig.Enabled {
    log.Println("START TORRENT")
    m.StartTorrentClient()
  }

	return nil
}

func (m *Manager) StartTorrentClient() error {
  config := torrent.NewDefaultClientConfig()
  config.SetListenAddr(":" + fmt.Sprint(m.torrentConfig.Port))
  config.Seed = true

  log.Println("DATA DIR: ", m.torrentConfig.DataDir)

  config.DataDir = m.torrentConfig.DataDir

  if _, err := os.Stat(m.torrentConfig.DataDir); os.IsNotExist(err) {
    os.MkdirAll(m.torrentConfig.DataDir, 0700)
  }


  // Instantiating the client will make it bootstrap and listen eagerly,
  // so no go routine is needed here
  client, err := torrent.NewClient(config)
  if err != nil {
    return err
  }
  m.torrentClient = client
  return nil
}

func (m *Manager) StopTorrentClient() []error{
  if m.TorrentClientStarted() {
    return m.torrentClient.Close()
  }
  return make([]error, 0)
}

func (m *Manager) TorrentClientStarted() bool {
  return m.torrentClient != nil
}

func (m *Manager) runENSVerificationLoop() {
	go func() {
		for {
			select {
			case <-m.quit:
				m.logger.Debug("quitting ens verification loop")
				return
			case records, more := <-m.ensSubscription:
				if !more {
					m.logger.Debug("no more ens records, quitting")
					return
				}
				m.logger.Info("received records", zap.Any("records", records))

			}
		}
	}()
}

func (m *Manager) Stop() error {
	close(m.quit)
	for _, c := range m.subscriptions {
		close(c)
	}

  for _, t := range m.messageArchiveCreationTasks {
    close(t)
  }
  m.StopTorrentClient()
	return nil
}

func (m *Manager) publish(subscription *Subscription) {
	for _, s := range m.subscriptions {
		select {
		case s <- subscription:
		default:
			m.logger.Warn("subscription channel full, dropping message")
		}
	}
}

func (m *Manager) All() ([]*Community, error) {
	return m.persistence.AllCommunities(m.identity)
}

func (m *Manager) Joined() ([]*Community, error) {
	return m.persistence.JoinedCommunities(m.identity)
}

func (m *Manager) JoinedAndPendingCommunitiesWithRequests() ([]*Community, error) {
	return m.persistence.JoinedAndPendingCommunitiesWithRequests(m.identity)
}

func (m *Manager) Created() ([]*Community, error) {
	return m.persistence.CreatedCommunities(m.identity)
}

// CreateCommunity takes a description, generates an ID for it, saves it and return it
func (m *Manager) CreateCommunity(request *requests.CreateCommunity) (*Community, error) {

	description, err := request.ToCommunityDescription()
	if err != nil {
		return nil, err
	}

	description.Members = make(map[string]*protobuf.CommunityMember)
	description.Members[common.PubkeyToHex(m.identity)] = &protobuf.CommunityMember{Roles: []protobuf.CommunityMember_Roles{protobuf.CommunityMember_ROLE_ALL}}

	err = ValidateCommunityDescription(description)
	if err != nil {
		return nil, err
	}

	description.Clock = 1

	key, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}

	config := Config{
		ID:                   &key.PublicKey,
		PrivateKey:           key,
		Logger:               m.logger,
		Joined:               true,
		MemberIdentity:       m.identity,
		CommunityDescription: description,
	}
	community, err := New(config)
	if err != nil {
		return nil, err
	}

	// We join any community we create
	community.Join()

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}

	m.publish(&Subscription{Community: community})

	return community, nil
}

// CreateCommunity takes a description, updates the community with the description,
// saves it and returns it
func (m *Manager) EditCommunity(request *requests.EditCommunity) (*Community, error) {
	community, err := m.GetByID(request.CommunityID)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, ErrOrgNotFound
	}
	if !community.IsAdmin() {
		return nil, errors.New("not an admin")
	}

	newDescription, err := request.ToCommunityDescription()
	if err != nil {
		return nil, fmt.Errorf("Can't create community description: %v", err)
	}

	// If permissions weren't explicitly set on original request, use existing ones
	if newDescription.Permissions.Access == protobuf.CommunityPermissions_UNKNOWN_ACCESS {
		newDescription.Permissions.Access = community.config.CommunityDescription.Permissions.Access
	}
	// If the image wasn't edited, use the existing one
	// NOTE: This will NOT allow deletion of the community image; it will need to
	// be handled separately.
	if request.Image == "" {
		newDescription.Identity.Images = community.config.CommunityDescription.Identity.Images
	}
	// TODO: handle delete image (if needed)

	err = ValidateCommunityDescription(newDescription)
	if err != nil {
		return nil, err
	}

	// Edit the community values
	community.Edit(newDescription)
	if err != nil {
		return nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}

	m.publish(&Subscription{Community: community})

	return community, nil
}

func (m *Manager) ExportCommunity(id types.HexBytes) (*ecdsa.PrivateKey, error) {
	community, err := m.GetByID(id)
	if err != nil {
		return nil, err
	}

	if !community.IsAdmin() {
		return nil, errors.New("not an admin")
	}

	return community.config.PrivateKey, nil
}

func (m *Manager) ImportCommunity(key *ecdsa.PrivateKey) (*Community, error) {
	communityID := crypto.CompressPubkey(&key.PublicKey)

	community, err := m.persistence.GetByID(m.identity, communityID)
	if err != nil {
		return nil, err
	}

	if community == nil {
		description := &protobuf.CommunityDescription{
			Permissions: &protobuf.CommunityPermissions{},
		}

		config := Config{
			ID:                   &key.PublicKey,
			PrivateKey:           key,
			Logger:               m.logger,
			Joined:               true,
			MemberIdentity:       m.identity,
			CommunityDescription: description,
		}
		community, err = New(config)
		if err != nil {
			return nil, err
		}
	} else {
		community.config.PrivateKey = key
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}

	return community, nil
}

func (m *Manager) CreateChat(communityID types.HexBytes, chat *protobuf.CommunityChat) (*Community, *CommunityChanges, error) {
	community, err := m.GetByID(communityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}
	chatID := uuid.New().String()
	changes, err := community.CreateChat(chatID, chat)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, changes, nil
}

func (m *Manager) EditChat(communityID types.HexBytes, chatID string, chat *protobuf.CommunityChat) (*Community, *CommunityChanges, error) {
	community, err := m.GetByID(communityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}

	// Remove communityID prefix from chatID if exists
	if strings.HasPrefix(chatID, communityID.String()) {
		chatID = strings.TrimPrefix(chatID, communityID.String())
	}

	changes, err := community.EditChat(chatID, chat)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, changes, nil
}

func (m *Manager) DeleteChat(communityID types.HexBytes, chatID string) (*Community, *protobuf.CommunityDescription, error) {
	community, err := m.GetByID(communityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}

	// Remove communityID prefix from chatID if exists
	if strings.HasPrefix(chatID, communityID.String()) {
		chatID = strings.TrimPrefix(chatID, communityID.String())
	}
	description, err := community.DeleteChat(chatID)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, description, nil
}

func (m *Manager) CreateCategory(request *requests.CreateCommunityCategory) (*Community, *CommunityChanges, error) {
	community, err := m.GetByID(request.CommunityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}
	categoryID := uuid.New().String()

	// Remove communityID prefix from chatID if exists
	for i, cid := range request.ChatIDs {
		if strings.HasPrefix(cid, request.CommunityID.String()) {
			request.ChatIDs[i] = strings.TrimPrefix(cid, request.CommunityID.String())
		}
	}

	changes, err := community.CreateCategory(categoryID, request.CategoryName, request.ChatIDs)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, changes, nil
}

func (m *Manager) EditCategory(request *requests.EditCommunityCategory) (*Community, *CommunityChanges, error) {
	community, err := m.GetByID(request.CommunityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}

	// Remove communityID prefix from chatID if exists
	for i, cid := range request.ChatIDs {
		if strings.HasPrefix(cid, request.CommunityID.String()) {
			request.ChatIDs[i] = strings.TrimPrefix(cid, request.CommunityID.String())
		}
	}

	changes, err := community.EditCategory(request.CategoryID, request.CategoryName, request.ChatIDs)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, changes, nil
}

func (m *Manager) ReorderCategories(request *requests.ReorderCommunityCategories) (*Community, *CommunityChanges, error) {
	community, err := m.GetByID(request.CommunityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}

	changes, err := community.ReorderCategories(request.CategoryID, request.Position)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, changes, nil
}

func (m *Manager) ReorderChat(request *requests.ReorderCommunityChat) (*Community, *CommunityChanges, error) {
	community, err := m.GetByID(request.CommunityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}

	// Remove communityID prefix from chatID if exists
	if strings.HasPrefix(request.ChatID, request.CommunityID.String()) {
		request.ChatID = strings.TrimPrefix(request.ChatID, request.CommunityID.String())
	}

	changes, err := community.ReorderChat(request.CategoryID, request.ChatID, request.Position)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, changes, nil
}

func (m *Manager) DeleteCategory(request *requests.DeleteCommunityCategory) (*Community, *CommunityChanges, error) {
	community, err := m.GetByID(request.CommunityID)
	if err != nil {
		return nil, nil, err
	}
	if community == nil {
		return nil, nil, ErrOrgNotFound
	}

	changes, err := community.DeleteCategory(request.CategoryID)
	if err != nil {
		return nil, nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, nil, err
	}

	// Advertise changes
	m.publish(&Subscription{Community: community})

	return community, changes, nil
}

func (m *Manager) HandleCommunityDescriptionMessage(signer *ecdsa.PublicKey, description *protobuf.CommunityDescription, payload []byte) (*CommunityResponse, error) {
	id := crypto.CompressPubkey(signer)
	community, err := m.persistence.GetByID(m.identity, id)
	if err != nil {
		return nil, err
	}

	if community == nil {
		config := Config{
			CommunityDescription:          description,
			Logger:                        m.logger,
			MarshaledCommunityDescription: payload,
			MemberIdentity:                m.identity,
			ID:                            signer,
		}

		community, err = New(config)
		if err != nil {
			return nil, err
		}
	}

	changes, err := community.UpdateCommunityDescription(signer, description, payload)
	if err != nil {
		return nil, err
	}

	pkString := common.PubkeyToHex(m.identity)

	// If the community require membership, we set whether we should leave/join the community after a state change
	if community.InvitationOnly() || community.OnRequest() {
		if changes.HasNewMember(pkString) {
			hasPendingRequest, err := m.persistence.HasPendingRequestsToJoinForUserAndCommunity(pkString, changes.Community.ID())
			if err != nil {
				return nil, err
			}
			// If there's any pending request, we should join the community
			// automatically
			changes.ShouldMemberJoin = hasPendingRequest
		}

		if changes.HasMemberLeft(pkString) {
			// If we joined previously the community, we should leave it
			changes.ShouldMemberLeave = community.Joined()
		}
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}

	// We mark our requests as completed, though maybe we should mark
	// any request for any user that has been added as completed
	if err := m.markRequestToJoin(m.identity, community); err != nil {
		return nil, err
	}
	// Check if there's a change and we should be joining

	return &CommunityResponse{
		Community: community,
		Changes:   changes,
	}, nil
}

// TODO: This is not fully implemented, we want to save the grant passed at
// this stage and make sure it's used when publishing.
func (m *Manager) HandleCommunityInvitation(signer *ecdsa.PublicKey, invitation *protobuf.CommunityInvitation, payload []byte) (*CommunityResponse, error) {
	m.logger.Debug("Handling wrapped community description message")

	community, err := m.HandleWrappedCommunityDescriptionMessage(payload)
	if err != nil {
		return nil, err
	}

	// Save grant

	return community, nil
}

// markRequestToJoin marks all the pending requests to join as completed
// if we are members
func (m *Manager) markRequestToJoin(pk *ecdsa.PublicKey, community *Community) error {
	if community.HasMember(pk) {
		return m.persistence.SetRequestToJoinState(common.PubkeyToHex(pk), community.ID(), RequestToJoinStateAccepted)
	}
	return nil
}

func (m *Manager) SetMuted(id types.HexBytes, muted bool) error {
	return m.persistence.SetMuted(id, muted)
}

func (m *Manager) AcceptRequestToJoin(request *requests.AcceptRequestToJoinCommunity) (*Community, error) {
	dbRequest, err := m.persistence.GetRequestToJoin(request.ID)
	if err != nil {
		return nil, err
	}

	community, err := m.GetByID(dbRequest.CommunityID)
	if err != nil {
		return nil, err
	}

	pk, err := common.HexToPubkey(dbRequest.PublicKey)
	if err != nil {
		return nil, err
	}

	return m.inviteUsersToCommunity(community, []*ecdsa.PublicKey{pk})
}

func (m *Manager) DeclineRequestToJoin(request *requests.DeclineRequestToJoinCommunity) error {
	dbRequest, err := m.persistence.GetRequestToJoin(request.ID)
	if err != nil {
		return err
	}

	return m.persistence.SetRequestToJoinState(dbRequest.PublicKey, dbRequest.CommunityID, RequestToJoinStateDeclined)

}

func (m *Manager) HandleCommunityRequestToJoin(signer *ecdsa.PublicKey, request *protobuf.CommunityRequestToJoin) (*RequestToJoin, error) {
	community, err := m.persistence.GetByID(m.identity, request.CommunityId)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, ErrOrgNotFound
	}

	// If they are already a member, ignore
	if community.HasMember(signer) {
		return nil, ErrAlreadyMember
	}

	if err := community.ValidateRequestToJoin(signer, request); err != nil {
		return nil, err
	}

	requestToJoin := &RequestToJoin{
		PublicKey:   common.PubkeyToHex(signer),
		Clock:       request.Clock,
		ENSName:     request.EnsName,
		CommunityID: request.CommunityId,
		State:       RequestToJoinStatePending,
	}

	requestToJoin.CalculateID()

	if err := m.persistence.SaveRequestToJoin(requestToJoin); err != nil {
		return nil, err
	}

	return requestToJoin, nil
}

func (m *Manager) HandleWrappedCommunityDescriptionMessage(payload []byte) (*CommunityResponse, error) {
	m.logger.Debug("Handling wrapped community description message")

	applicationMetadataMessage := &protobuf.ApplicationMetadataMessage{}
	err := proto.Unmarshal(payload, applicationMetadataMessage)
	if err != nil {
		return nil, err
	}
	if applicationMetadataMessage.Type != protobuf.ApplicationMetadataMessage_COMMUNITY_DESCRIPTION {
		return nil, ErrInvalidMessage
	}
	signer, err := applicationMetadataMessage.RecoverKey()
	if err != nil {
		return nil, err
	}

	description := &protobuf.CommunityDescription{}

	err = proto.Unmarshal(applicationMetadataMessage.Payload, description)
	if err != nil {
		return nil, err
	}

	return m.HandleCommunityDescriptionMessage(signer, description, payload)
}

func (m *Manager) HandleCommunityMessageArchiveMagnetlink(signer *ecdsa.PublicKey, magnetlink string) error {

	id := types.HexBytes(crypto.CompressPubkey(signer))
  log.Println("HANDING MAGNET LINK FOR: ", id.String())
  m.UnseedHistoryArchiveTorrent(id)
  go m.DownloadHistoryArchiveTorrentByMagnetlink(id, magnetlink)
  // if err != nil {
  //   return err
  // }

  return nil
}

func (m *Manager) JoinCommunity(id types.HexBytes) (*Community, error) {
	community, err := m.GetByID(id)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, ErrOrgNotFound
	}
	community.Join()
	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}
	return community, nil
}

func (m *Manager) LeaveCommunity(id types.HexBytes) (*Community, error) {
	community, err := m.GetByID(id)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, ErrOrgNotFound
	}
	if community.IsAdmin() {
		_, err = community.RemoveUserFromOrg(m.identity)
		if err != nil {
			return nil, err
		}
	}
	community.Leave()
	err = m.persistence.SaveCommunity(community)

	if err != nil {
		return nil, err
	}
	return community, nil
}

func (m *Manager) inviteUsersToCommunity(community *Community, pks []*ecdsa.PublicKey) (*Community, error) {
	var invitations []*protobuf.CommunityInvitation
	for _, pk := range pks {
		invitation, err := community.InviteUserToOrg(pk)
		if err != nil {
			return nil, err
		}
		// We mark the user request (if any) as completed
		if err := m.markRequestToJoin(pk, community); err != nil {
			return nil, err
		}

		invitations = append(invitations, invitation)
	}

	err := m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}

	m.publish(&Subscription{Community: community, Invitations: invitations})

	return community, nil
}

func (m *Manager) InviteUsersToCommunity(communityID types.HexBytes, pks []*ecdsa.PublicKey) (*Community, error) {
	community, err := m.GetByID(communityID)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, ErrOrgNotFound
	}

	return m.inviteUsersToCommunity(community, pks)
}

func (m *Manager) RemoveUserFromCommunity(id types.HexBytes, pk *ecdsa.PublicKey) (*Community, error) {
	community, err := m.GetByID(id)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, ErrOrgNotFound
	}

	_, err = community.RemoveUserFromOrg(pk)
	if err != nil {
		return nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}

	m.publish(&Subscription{Community: community})

	return community, nil
}

func (m *Manager) BanUserFromCommunity(request *requests.BanUserFromCommunity) (*Community, error) {
	id := request.CommunityID

	publicKey, err := common.HexToPubkey(request.User.String())
	if err != nil {
		return nil, err
	}

	community, err := m.GetByID(id)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, ErrOrgNotFound
	}

	_, err = community.BanUserFromCommunity(publicKey)
	if err != nil {
		return nil, err
	}

	err = m.persistence.SaveCommunity(community)
	if err != nil {
		return nil, err
	}

	m.publish(&Subscription{Community: community})

	return community, nil
}

func (m *Manager) GetByID(id []byte) (*Community, error) {
	return m.persistence.GetByID(m.identity, id)
}

func (m *Manager) GetByIDString(idString string) (*Community, error) {
	id, err := types.DecodeHex(idString)
	if err != nil {
		return nil, err
	}
	return m.GetByID(id)
}

func (m *Manager) RequestToJoin(requester *ecdsa.PublicKey, request *requests.RequestToJoinCommunity) (*Community, *RequestToJoin, error) {
	community, err := m.persistence.GetByID(m.identity, request.CommunityID)
	if err != nil {
		return nil, nil, err
	}

	// We don't allow requesting access if already a member
	if community.HasMember(m.identity) {
		return nil, nil, ErrAlreadyMember
	}

	clock := uint64(time.Now().Unix())
	requestToJoin := &RequestToJoin{
		PublicKey:   common.PubkeyToHex(requester),
		Clock:       clock,
		ENSName:     request.ENSName,
		CommunityID: request.CommunityID,
		State:       RequestToJoinStatePending,
		Our:         true,
	}

	requestToJoin.CalculateID()

	if err := m.persistence.SaveRequestToJoin(requestToJoin); err != nil {
		return nil, nil, err
	}
	community.config.RequestedToJoinAt = uint64(time.Now().Unix())
	community.AddRequestToJoin(requestToJoin)

	return community, requestToJoin, nil
}

func (m *Manager) SaveRequestToJoin(request *RequestToJoin) error {
	return m.persistence.SaveRequestToJoin(request)
}

func (m *Manager) PendingRequestsToJoinForUser(pk *ecdsa.PublicKey) ([]*RequestToJoin, error) {
	return m.persistence.PendingRequestsToJoinForUser(common.PubkeyToHex(pk))
}

func (m *Manager) PendingRequestsToJoinForCommunity(id types.HexBytes) ([]*RequestToJoin, error) {
	m.logger.Info("fetching pending invitations", zap.String("community-id", id.String()))
	return m.persistence.PendingRequestsToJoinForCommunity(id)
}

func (m *Manager) CanPost(pk *ecdsa.PublicKey, communityID string, chatID string, grant []byte) (bool, error) {
	community, err := m.GetByIDString(communityID)
	if err != nil {
		return false, err
	}
	if community == nil {
		return false, nil
	}
	return community.CanPost(pk, chatID, grant)
}

func (m *Manager) ShouldHandleSyncCommunity(community *protobuf.SyncCommunity) (bool, error) {
	return m.persistence.ShouldHandleSyncCommunity(community)
}

func (m *Manager) SetSyncClock(id []byte, clock uint64) error {
	return m.persistence.SetSyncClock(id, clock)
}

func (m *Manager) SetPrivateKey(id []byte, privKey *ecdsa.PrivateKey) error {
	return m.persistence.SetPrivateKey(id, privKey)
}

func (m *Manager) GetSyncedRawCommunity(id []byte) (*rawCommunityRow, error) {
	return m.persistence.getSyncedRawCommunity(id)
}

func (m *Manager) GetAdminCommunitiesChatIDs() (map[string]bool, error) {
  adminCommunities, err := m.Created()
  if err != nil {
    return nil, err
  }

  chatIDs := make(map[string]bool)
  for _, c := range adminCommunities {
    if c.Joined() {
      for _, id := range c.ChatIDs() {
        chatIDs[id] = true
      }
    }
  }
  return chatIDs, nil
}

func (m *Manager) IsAdminCommunity(pubKey *ecdsa.PublicKey) (bool, error) {
  adminCommunities, err := m.Created()
  if err != nil {
    return false, err
  }

  for _, c := range adminCommunities {
    if c.PrivateKey().PublicKey.Equal(pubKey) {
      return true, nil
    }
  }
  return false, nil
}

func (m *Manager) IsJoinedCommunity(pubKey *ecdsa.PublicKey) (bool, error) {
  community, err := m.GetByID(crypto.CompressPubkey(pubKey))
  if err != nil {
    return false, err
  }

  return community != nil && community.Joined(), nil
}

func (m *Manager) GetCommunityChatsFilters(communityID types.HexBytes) ([]*transport.Filter, error) {
  chatIDs, err := m.persistence.GetCommunityChatIDs(communityID)
  if err != nil {
    return nil, err
  }

  filters := []*transport.Filter{}
  for _, cid := range chatIDs {
    filters = append(filters, m.transport.FilterByChatID(cid))
  }
  return filters, nil
}

func (m *Manager) GetCommunityChatsTopics(communityID types.HexBytes) ([]types.TopicType, error) {
  filters, err := m.GetCommunityChatsFilters(communityID)
  if err != nil {
    return nil, err
  }

  topics := []types.TopicType{}
  for _, filter := range filters {
    topics = append(topics, filter.Topic)
  }

  return topics, nil
}

func (m *Manager) StoreWakuMessage(message *types.Message) error {
  return m.persistence.SaveWakuMessage(message)
}

func (m *Manager) GetLatestWakuMessageTimestamp(topics []types.TopicType) (uint64, error) {
  return m.persistence.GetLatestWakuMessageTimestamp(topics)
}

func (m *Manager) GetOldestWakuMessageTimestamp(topics []types.TopicType) (uint64, error) {
  return m.persistence.GetOldestWakuMessageTimestamp(topics)
}

func (m *Manager) GetLastMessageArchiveEndDate(communityID types.HexBytes) (uint64, error) {
  return m.persistence.GetLastMessageArchiveEndDate(communityID)
}

func (m *Manager) GetHistoryArchivePartitionStartTimestamp(communityID types.HexBytes) (uint64, error) {
  filters, err := m.GetCommunityChatsFilters(communityID)
  if err != nil {
    m.logger.Warn("failed to get community chats filters", zap.Error(err))
    return 0, err
  }

  if len(filters) == 0 {
    // If we don't have chat filters, we likely don't have any chats
    // associated to this community, which means there's nothing more
    // to do here
    return 0, nil
  }

  topics := []types.TopicType{}

  for _, filter := range filters {
    topics = append(topics, filter.Topic)
  }

  lastArchiveEndDateTimestamp, err := m.GetLastMessageArchiveEndDate(communityID)
  if err != nil {
    m.logger.Debug("failed to get last archive end date", zap.Error(err))
    return 0, err
  }

  if lastArchiveEndDateTimestamp == 0 {
    // If we don't have a tracked last message archive end date, it
    // means we haven't created an archive before, which means
    // the next thing to look at is the oldest waku message timestamp for
    // this community
    lastArchiveEndDateTimestamp, err = m.GetOldestWakuMessageTimestamp(topics)
    if err != nil {
      m.logger.Warn("failed to get oldest waku message timestamp", zap.Error(err))
      return 0, err
    }
    if lastArchiveEndDateTimestamp == 0 {
      // This means there's no waku message stored for this community so far
      // (even after requesting possibly missed messages), so no messages exist yet that can be archived
      return 0, nil
    }
  }

  return lastArchiveEndDateTimestamp, nil
}


func (m *Manager) RunHistoryArchiveCreationInterval(community *Community, interval time.Duration, dispatchMagnetlink func(types.HexBytes)) {

  _, exists := m.messageArchiveCreationTasks[community.IDString()]

  if exists {
    return
  }

  cancel := make(chan struct{})
  m.messageArchiveCreationTasks[community.IDString()] = cancel

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
      case <-ticker.C:
        lastArchiveEndDateTimestamp, err := m.GetHistoryArchivePartitionStartTimestamp(community.ID())
        if err != nil {
          m.logger.Debug("failed to get last archive end date", zap.Error(err))
          continue
        }

        if lastArchiveEndDateTimestamp == 0 {
          // This means there are no waku messages for this community,
          // so nothing to do here
          continue
        }

        topics, err := m.GetCommunityChatsTopics(community.ID())
        if err != nil {
          m.logger.Debug("failed to get community chats topics", zap.Error(err))
          continue
        }

        to := time.Now()
        lastArchiveEndDate := time.Unix(int64(lastArchiveEndDateTimestamp), 0)

        m.UnseedHistoryArchiveTorrent(community.ID())
        err = m.CreateHistoryArchiveTorrent(community.ID(), topics, lastArchiveEndDate, to, interval)
        if err != nil {
          m.logger.Debug("failed to create history archive torrent", zap.Error(err))
          continue
        }
        m.SeedHistoryArchiveTorrent(community.ID())
        dispatchMagnetlink(community.ID())
      case <-cancel:
        m.UnseedHistoryArchiveTorrent(community.ID())
        delete(m.messageArchiveCreationTasks, community.IDString())
        return
    }
  }
}

type EncodedArchiveData struct {
  padding int
  bytes []byte
}

func (m *Manager) CreateHistoryArchiveTorrent(communityID types.HexBytes, topics []types.TopicType, startDate time.Time, endDate time.Time, partition time.Duration) error {

  from := startDate
  to := from.Add(partition)

  archiveDir := m.torrentConfig.DataDir + "/" + communityID.String()
  torrentDir := m.torrentConfig.TorrentDir
  indexPath := archiveDir + "/index"
  dataPath := archiveDir + "/data"

  wakuMessageArchiveIndexProto := &protobuf.WakuMessageArchiveIndex{}
  wakuMessageArchiveIndex := make(map[string]*protobuf.WakuMessageArchiveIndexMetadata)

  if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
    os.MkdirAll(archiveDir, 0700)
  }
  if _, err := os.Stat(torrentDir); os.IsNotExist(err) {
    os.MkdirAll(torrentDir, 0700)
  }

  _, err := os.Stat(indexPath)
  if err == nil {
    wakuMessageArchiveIndexProto, err = m.loadCommunityArchiveIndexFromFile(communityID)
    if err != nil {
      return err
    }
  }

  var offset uint64 = 0

  for hash, metadata := range wakuMessageArchiveIndexProto.Archives {
    offset = offset + metadata.Size
    wakuMessageArchiveIndex[hash] = metadata
    // d, _ := os.ReadFile(dataPath)

    // a := &protobuf.WakuMessageArchive{}
    // o := metadata.Offset
    // s := metadata.Size
    // p := metadata.Padding
    // log.Println("OFFSET: ", o)
    // log.Println("SIZE: ", s)
    // log.Println("PADDING: ", p)
    // f := d[o:o+s-p]
    // err := proto.Unmarshal(f, a)
    // if err != nil {
    //   log.Println("ERROR: ", err)
    //   return err
    // }
    // log.Println("YAY ARCHIVE RECOVERED: ", a)
  }

  var encodedArchives []*EncodedArchiveData
  topicsAsByteArrays := topicsAsByteArrays(topics)

  for {
    if endDate.Sub(from) < partition {
      break
    }
    messages, err := m.persistence.GetWakuMessagesByFilterTopic(topics, uint64(from.Unix()), uint64(to.Unix()))
    if err != nil {
      return err
    }

    wakuMessageArchive := m.createWakuMessageArchive(from, to, messages, topicsAsByteArrays)
    encodedArchive, err := proto.Marshal(wakuMessageArchive)
    if err != nil {
      return err
    }

    rawSize := len(encodedArchive)
    padding := 0
    size := 0

    if rawSize > pieceLength {
      size = rawSize + pieceLength - (rawSize % pieceLength)
      padding = size - rawSize
    } else {
      padding = pieceLength - rawSize
      size = rawSize + padding
    }

    wakuMessageArchiveIndexMetadata := &protobuf.WakuMessageArchiveIndexMetadata{
      Metadata: wakuMessageArchive.Metadata,
      Offset: uint64(offset),
      Size: uint64(size),
      Padding: uint64(padding),
    }

    wakuMessageArchiveIndexMetadataBytes, err := proto.Marshal(wakuMessageArchiveIndexMetadata)
    if err != nil {
      return err
    }

    wakuMessageArchiveIndex[crypto.Keccak256Hash(wakuMessageArchiveIndexMetadataBytes).String()] = wakuMessageArchiveIndexMetadata
    encodedArchives = append(encodedArchives, &EncodedArchiveData{ bytes: encodedArchive, padding: padding })
    from = to
    to = to.Add(partition)
    offset = uint64(offset) + uint64(rawSize) + uint64(padding)
  }

  if len(encodedArchives) > 0 {

    dataBytes := make([]byte, 0)
    if _, err := os.Stat(dataPath); err == nil {
      dataBytes, err = os.ReadFile(dataPath)
      if err != nil {
        return err
      }
    }

    for _, encodedArchiveData := range encodedArchives {
      dataBytes = append(dataBytes, encodedArchiveData.bytes...)
      dataBytes = append(dataBytes, make([]byte, encodedArchiveData.padding)...)
    }

    wakuMessageArchiveIndexProto.Archives = wakuMessageArchiveIndex
    indexBytes, err := proto.Marshal(wakuMessageArchiveIndexProto)
    if err != nil {
      return err
    }

    err = os.WriteFile(indexPath, indexBytes, 0644)
    if err != nil {
      return err
    }

    err = os.WriteFile(dataPath, dataBytes, 0644)
    if err != nil {
      return err
    }

    metaInfo := metainfo.MetaInfo{
      AnnounceList: defaultAnnounceList,
    }
    metaInfo.SetDefaults()
    metaInfo.CreatedBy = common.PubkeyToHex(m.identity)

    info := metainfo.Info{
      PieceLength: int64(pieceLength),
    }

    err = info.BuildFromFilePath(archiveDir)
    if err != nil {
      return err
    }

    metaInfo.InfoBytes, err = bencode.Marshal(info)
    if err != nil {
      return err
    }

    metaInfoBytes, err := bencode.Marshal(metaInfo)
    if err != nil {
      return err
    }

    err = os.WriteFile(m.torrentFile(communityID.String()), metaInfoBytes, 0644)
    if err != nil {
      return err
    }

    err = m.persistence.UpdateLastMessageArchiveEndDate(communityID, uint64(from.Unix()))
    if err != nil {
      return err
    }
  }
  return nil
}

func (m *Manager) SeedHistoryArchiveTorrent(communityID types.HexBytes) error {
  m.UnseedHistoryArchiveTorrent(communityID)

  id := communityID.String()
  torrentFile := m.torrentFile(id)

  metaInfo, err := metainfo.LoadFromFile(torrentFile)
  if err != nil {
    return err
  }

  info, err := metaInfo.UnmarshalInfo()
  if err != nil {
    return err
  }

  hash := metaInfo.HashInfoBytes()
  m.torrentTasks[id] = hash

  if err != nil {
    return err
  }

  torrent, err := m.torrentClient.AddTorrent(metaInfo)
  if err != nil {
    return err
  }
  torrent.DownloadAll()

  log.Println("SEEDING: ", metaInfo.Magnet(nil, &info).String())
  return nil
}

func (m *Manager) UnseedHistoryArchiveTorrent(communityID types.HexBytes) {
  id := communityID.String()
  hash, exists := m.torrentTasks[id]

  if exists {
    torrent, ok := m.torrentClient.Torrent(hash)
    if ok {
      torrent.Drop()
      delete(m.torrentTasks, id)
    }
  }
}

func (m *Manager) DownloadHistoryArchiveTorrentByMagnetlink(communityID types.HexBytes, magnetlink string) error {

  id := communityID.String()
  ml, _ := metainfo.ParseMagnetUri(magnetlink)

  torrent, _ := m.torrentClient.AddMagnet(magnetlink)
  m.torrentTasks[id] = ml.InfoHash

  select {
    case <-torrent.GotInfo():
      files := torrent.Files()

      indexFile := files[1]
      indexFile.Download()

      for {
        if indexFile.BytesCompleted() == indexFile.Length() {
          break;
        }
      }

      index, err := m.loadCommunityArchiveIndexFromFile(communityID)
      if err != nil {
        return err
      }

      for hash, metadata := range index.Archives {
        hasArchive, err := m.persistence.HasMessageArchiveID(communityID, hash)
        if err != nil {
          return err
        }
        if hasArchive {
          continue
        }

        startIndex := int(metadata.Offset)/int(pieceLength)
        endIndex := startIndex + int(metadata.Size)/int(pieceLength)-1

        torrent.DownloadPieces(startIndex, endIndex+1)

        log.Println("DOWNLOADING DATA FOR ARCHIVE: ", hash)
        psc := torrent.SubscribePieceStateChanges()
        for {
          i := startIndex
          done := false
          for {
            if i > endIndex {
              break
            }
            done = torrent.PieceState(i).Complete
            i++
          }
          if done {
            psc.Close()
            break
          }
          <-psc.Values
        }
        log.Println("DONE!")

        totalData, err := os.ReadFile(m.archiveDataFile(id))
        if err != nil {
          return err
        }

        archive := &protobuf.WakuMessageArchive{}
        data := totalData[metadata.Offset:metadata.Offset+metadata.Size-metadata.Padding]
        err = proto.Unmarshal(data, archive)
        if err != nil {
          log.Println("ERROR: ", err)
          return err
        }
        log.Println("ARCHIVE(" +hash+ "): ", archive)
        log.Println("SAVING ARCHIVE HASH: ", hash)
        err = m.persistence.SaveMessageArchiveID(communityID, hash)
        if err != nil {
          log.Println("ERROR: ", err)
          return err
        }
        break
      }
      return nil
  }
}

func (m *Manager) GetHistoryArchiveMagnetlink(communityID types.HexBytes) (string, error) {
  id := communityID.String()
  torrentFile := m.torrentFile(id)

  metaInfo, err := metainfo.LoadFromFile(torrentFile)
  if err != nil {
    return "", err
  }

  info, err := metaInfo.UnmarshalInfo()
  if err != nil {
    return "", err
  }

  return metaInfo.Magnet(nil, &info).String(), nil
}

func (m *Manager) createWakuMessageArchive(from time.Time, to time.Time, messages []types.Message, topics [][]byte) *protobuf.WakuMessageArchive {
  var wakuMessages []*protobuf.WakuMessage

  for _, msg := range messages {
    topic := topicTypeToByteArray(msg.Topic)
    wakuMessage := &protobuf.WakuMessage{
      Sig: msg.Sig,
      Timestamp: uint64(msg.Timestamp),
      Topic: topic,
      Payload: msg.Payload,
      Padding: msg.Padding,
      Hash: msg.Hash,
    }
    wakuMessages = append(wakuMessages, wakuMessage)
  }

  metadata := protobuf.WakuMessageArchiveMetadata{
    From: uint64(from.Unix()),
    To: uint64(to.Unix()),
    ContentTopic: topics,
  }

  wakuMessageArchive := &protobuf.WakuMessageArchive{
    Metadata: &metadata,
    Messages: wakuMessages,
  }
  return wakuMessageArchive
}

func (m *Manager) loadCommunityArchiveIndexFromFile(communityID types.HexBytes) (*protobuf.WakuMessageArchiveIndex, error) {
    wakuMessageArchiveIndexProto := &protobuf.WakuMessageArchiveIndex{}

    indexPath := m.archiveIndexFile(communityID.String())
    indexData, err := os.ReadFile(indexPath)
    if err != nil {
      return nil, err
    }

    err = proto.Unmarshal(indexData, wakuMessageArchiveIndexProto)
    if err != nil {
      return nil, err
    }
    return wakuMessageArchiveIndexProto, nil
}

func (m *Manager) torrentFile(communityID string) string {
  return m.torrentConfig.TorrentDir + "/" + communityID + ".torrent"
}

func (m *Manager) archiveIndexFile(communityID string) string {
  return m.torrentConfig.DataDir + "/" + communityID + "/index"
}

func (m *Manager) archiveDataFile(communityID string) string {
  return m.torrentConfig.DataDir + "/" + communityID + "/data"
}

func topicsAsByteArrays (topics []types.TopicType) [][]byte {
  var topicsAsByteArrays [][]byte
  for _, t := range topics {
    topic := topicTypeToByteArray(t)
    topicsAsByteArrays = append(topicsAsByteArrays, topic)
  }
  return topicsAsByteArrays
}

func topicTypeToByteArray (t types.TopicType) []byte {
  topic := make([]byte, 4)
  for i, b := range t {
    topic[i] = b
  }
  return topic
}


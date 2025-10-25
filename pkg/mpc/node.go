package mpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/messaging"
	"github.com/fystack/mpcium/pkg/mpc/core"
	"github.com/fystack/mpcium/pkg/mpc/ecdsa"
	"github.com/fystack/mpcium/pkg/mpc/eddsa"
	"github.com/fystack/mpcium/pkg/node"
	"github.com/fystack/mpcium/pkg/storage"
)

const (
	PurposeKeygen    string = "keygen"
	PurposeSign      string = "sign"
	PurposeResharing string = "resharing"

	BackwardCompatibleVersion int = 0
	DefaultVersion            int = 1
)

type ID string

type Node struct {
	nodeID  string
	peerIDs []string

	preParams    []*keygen.LocalPreParams
	pubSub       messaging.PubSub
	direct       messaging.DirectMessaging
	KVstore      storage.Store
	keyinfoStore node.KeyStore

	identityStore node.IdentityStore

	peerRegistry PeerRegistry
}

func NewNode(
	nodeID string,
	peerIDs []string,
	pubSub messaging.PubSub,
	direct messaging.DirectMessaging,
	KVstore storage.Store,
	keyinfoStore node.KeyStore,
	peerRegistry PeerRegistry,
	identityStore node.IdentityStore,
) *Node {
	start := time.Now()
	elapsed := time.Since(start)
	logger.Info("Starting new node, preparams is generated successfully!", "elapsed", elapsed.Milliseconds())

	node := &Node{
		nodeID:        nodeID,
		peerIDs:       peerIDs,
		pubSub:        pubSub,
		direct:        direct,
		KVstore:       KVstore,
		keyinfoStore:  keyinfoStore,
		peerRegistry:  peerRegistry,
		identityStore: identityStore,
	}
	node.preParams = node.generatePreParams()

	// Start watching peers - ECDH is now handled by the registry
	go peerRegistry.WatchPeersReady()
	return node
}

func (p *Node) ID() string {
	return p.nodeID
}

func (p *Node) CreateKeyGenSession(
	sessionType core.SessionType,
	walletID string,
	threshold int,
	resultQueue messaging.MessageQueue,
) (core.KeyGenSession, error) {
	if !p.peerRegistry.ArePeersReady() {
		return nil, errors.New("all nodes are not ready")
	}

	keyInfo, _ := p.getKeyInfo(sessionType, walletID)
	if keyInfo != nil {
		return nil, fmt.Errorf("key already exists: %s", walletID)
	}

	switch sessionType {
	case core.SessionTypeECDSA:
		return p.createECDSAKeyGenSession(walletID, threshold, DefaultVersion, resultQueue)
	case core.SessionTypeEDDSA:
		return p.createEDDSAKeyGenSession(walletID, threshold, DefaultVersion, resultQueue)
	default:
		return nil, fmt.Errorf("unknown session type: %s", sessionType)
	}
}

func (p *Node) createECDSAKeyGenSession(walletID string, threshold int, version int, resultQueue messaging.MessageQueue) (core.KeyGenSession, error) {
	readyPeerIDs := p.peerRegistry.GetReadyPeersIncludeSelf()
	selfPartyID, allPartyIDs := p.keygenPartyIDs(PurposeKeygen, readyPeerIDs, version)
	session := ecdsa.NewECDSAKeygenSession(
		walletID,
		p.pubSub,
		p.direct,
		readyPeerIDs,
		selfPartyID,
		allPartyIDs,
		threshold,
		p.preParams[0],
		p.KVstore,
		p.keyinfoStore,
		resultQueue,
		p.identityStore,
	)
	return session, nil
}

func (p *Node) createEDDSAKeyGenSession(walletID string, threshold int, version int, resultQueue messaging.MessageQueue) (core.KeyGenSession, error) {
	readyPeerIDs := p.peerRegistry.GetReadyPeersIncludeSelf()
	selfPartyID, allPartyIDs := p.keygenPartyIDs(PurposeKeygen, readyPeerIDs, version)
	session := eddsa.NewEDDSAKeygenSession(
		walletID,
		p.pubSub,
		p.direct,
		readyPeerIDs,
		selfPartyID,
		allPartyIDs,
		threshold,
		p.KVstore,
		p.keyinfoStore,
		resultQueue,
		p.identityStore,
	)
	return session, nil
}

func (p *Node) CreateSigningSession(
	sessionType core.SessionType,
	walletID string,
	txID string,
	resultQueue messaging.MessageQueue,
	idempotentKey string,
) (core.SigningSession, error) {
	version := p.getVersion(sessionType, walletID)
	keyInfo, err := p.getKeyInfo(sessionType, walletID)
	if err != nil {
		return nil, err
	}

	readyPeers := p.peerRegistry.GetReadyPeersIncludeSelf()
	readyParticipantIDs := p.getReadyPeersForSession(keyInfo, readyPeers)

	logger.Info("Creating signing session",
		"type", sessionType,
		"readyPeers", readyPeers,
		"participantPeerIDs", keyInfo.ParticipantPeerIDs,
		"ready count", len(readyParticipantIDs),
		"min ready", keyInfo.Threshold+1,
		"version", version,
	)

	if len(readyParticipantIDs) < keyInfo.Threshold+1 {
		return nil, fmt.Errorf("not enough peers to create signing session! expected %d, got %d", keyInfo.Threshold+1, len(readyParticipantIDs))
	}

	if err := p.ensureNodeIsParticipant(keyInfo); err != nil {
		return nil, err
	}

	selfPartyID, allPartyIDs := p.keygenPartyIDs(PurposeKeygen, readyParticipantIDs, version)

	switch sessionType {
	case core.SessionTypeECDSA:
		return ecdsa.NewECDSASigningSession(
			walletID,
			txID,
			p.pubSub,
			p.direct,
			readyParticipantIDs,
			selfPartyID,
			allPartyIDs,
			keyInfo.Threshold,
			p.preParams[0],
			p.KVstore,
			p.keyinfoStore,
			resultQueue,
			p.identityStore,
			idempotentKey,
		), nil

	case core.SessionTypeEDDSA:
		return eddsa.NewEDDSASigningSession(
			walletID,
			txID,
			p.pubSub,
			p.direct,
			readyParticipantIDs,
			selfPartyID,
			allPartyIDs,
			keyInfo.Threshold,
			p.KVstore,
			p.keyinfoStore,
			resultQueue,
			p.identityStore,
			idempotentKey,
		), nil
	}

	return nil, errors.New("unknown session type")
}

// keygenPartyIDs generates the party IDs for key generation
// It returns the self party ID and all party IDs
// It also sorts the party IDs in place
func (n *Node) keygenPartyIDs(
	label string,
	readyPeerIDs []string,
	version int,
) (self *tss.PartyID, all []*tss.PartyID) {
	// Pre-allocate slice with exact size needed
	partyIDs := make([]*tss.PartyID, 0, len(readyPeerIDs))

	// Create all party IDs in one pass
	for _, peerID := range readyPeerIDs {
		partyID := core.CreatePartyID(peerID, label, version)
		if peerID == n.nodeID {
			self = partyID
		}
		partyIDs = append(partyIDs, partyID)
	}

	// Sort party IDs in place
	all = tss.SortPartyIDs(partyIDs, 0)
	return
}

func (p *Node) getKeyInfo(sessionType core.SessionType, walletID string) (*node.KeyInfo, error) {
	var keyID string
	switch sessionType {
	case core.SessionTypeECDSA:
		keyID = fmt.Sprintf("ecdsa:%s", walletID)
	case core.SessionTypeEDDSA:
		keyID = fmt.Sprintf("eddsa:%s", walletID)
	default:
		return nil, errors.New("unsupported session type")
	}
	return p.keyinfoStore.Get(keyID)
}

func (p *Node) getReadyPeersForSession(keyInfo *node.KeyInfo, readyPeers []string) []string {
	// Ensure all participants are ready
	readyParticipantIDs := make([]string, 0, len(keyInfo.ParticipantPeerIDs))
	for _, peerID := range keyInfo.ParticipantPeerIDs {
		if slices.Contains(readyPeers, peerID) {
			readyParticipantIDs = append(readyParticipantIDs, peerID)
		}
	}

	return readyParticipantIDs
}

func (p *Node) ensureNodeIsParticipant(keyInfo *node.KeyInfo) error {
	if !slices.Contains(keyInfo.ParticipantPeerIDs, p.nodeID) {
		return core.ErrNotInParticipantList
	}
	return nil
}

func (p *Node) CreateResharingSession(
	sessionType core.SessionType,
	walletID string,
	newThreshold int,
	newPeerIDs []string,
	isNewPeer bool,
	resultQueue messaging.MessageQueue,
) (core.ResharingSession, error) {
	// 1. Check peer readiness
	count := p.peerRegistry.GetReadyPeersCount()
	if count < int64(newThreshold)+1 {
		return nil, fmt.Errorf(
			"not enough peers to create reshare session! Expected at least %d, got %d",
			newThreshold+1,
			count,
		)
	}

	if len(newPeerIDs) < newThreshold+1 {
		return nil, fmt.Errorf("new peer list is smaller than required t+1")
	}

	// 2. Make sure all new peers are ready
	readyNewPeerIDs := p.peerRegistry.GetReadyPeersIncludeSelf()
	for _, peerID := range newPeerIDs {
		if !slices.Contains(readyNewPeerIDs, peerID) {
			return nil, fmt.Errorf("new peer %s is not ready", peerID)
		}
	}

	// 3. Load old key info
	keyPrefix, err := sessionKeyPrefix(sessionType)
	if err != nil {
		return nil, fmt.Errorf("failed to get session key prefix: %w", err)
	}
	keyInfoKey := fmt.Sprintf("%s:%s", keyPrefix, walletID)
	oldKeyInfo, err := p.keyinfoStore.Get(keyInfoKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get old key info: %w", err)
	}

	readyPeers := p.peerRegistry.GetReadyPeersIncludeSelf()
	readyOldParticipantIDs := p.getReadyPeersForSession(oldKeyInfo, readyPeers)

	isInOldCommittee := slices.Contains(oldKeyInfo.ParticipantPeerIDs, p.nodeID)
	isInNewCommittee := slices.Contains(newPeerIDs, p.nodeID)

	// 4. Skip if not relevant
	if isNewPeer && !isInNewCommittee {
		logger.Info("Skipping new session: node is not in new committee", "walletID", walletID, "nodeID", p.nodeID)
		return nil, nil
	}
	if !isNewPeer && !isInOldCommittee {
		logger.Info("Skipping old session: node is not in old committee", "walletID", walletID, "nodeID", p.nodeID)
		return nil, nil
	}

	logger.Info("Creating resharing session",
		"type", sessionType,
		"readyPeers", readyPeers,
		"participantPeerIDs", oldKeyInfo.ParticipantPeerIDs,
		"ready count", len(readyOldParticipantIDs),
		"min ready", oldKeyInfo.Threshold+1,
		"version", oldKeyInfo.Version,
		"isNewPeer", isNewPeer,
	)

	if len(readyOldParticipantIDs) < oldKeyInfo.Threshold+1 {
		return nil, fmt.Errorf("not enough peers to create resharing session! expected %d, got %d", oldKeyInfo.Threshold+1, len(readyOldParticipantIDs))
	}

	if !isNewPeer {
		if err := p.ensureNodeIsParticipant(oldKeyInfo); err != nil {
			return nil, err
		}
	}

	// 5. Generate party IDs
	version := p.getVersion(sessionType, walletID)
	oldSelf, oldAllPartyIDs := p.keygenPartyIDs(PurposeKeygen, readyOldParticipantIDs, version)
	newSelf, newAllPartyIDs := p.keygenPartyIDs(PurposeResharing, newPeerIDs, version+1)

	// 6. Pick identity and call session constructor
	var selfPartyID *tss.PartyID
	var participantPeerIDs []string
	if isNewPeer {
		selfPartyID = newSelf
		participantPeerIDs = newPeerIDs
	} else {
		selfPartyID = oldSelf
		participantPeerIDs = readyOldParticipantIDs
	}

	switch sessionType {
	case core.SessionTypeECDSA:
		preParams := p.preParams[0]
		if isNewPeer {
			preParams = p.preParams[1]
			participantPeerIDs = newPeerIDs
		} else {
			participantPeerIDs = oldKeyInfo.ParticipantPeerIDs
		}

		return ecdsa.NewECDSAResharingSession(
			walletID,
			p.pubSub,
			p.direct,
			participantPeerIDs,
			selfPartyID,
			oldAllPartyIDs,
			newAllPartyIDs,
			oldKeyInfo.Threshold,
			newThreshold,
			preParams,
			p.KVstore,
			p.keyinfoStore,
			resultQueue,
			p.identityStore,
			newPeerIDs,
			isNewPeer,
			oldKeyInfo.Version,
		), nil

	case core.SessionTypeEDDSA:
		return eddsa.NewEDDSAResharingSession(
			walletID,
			p.pubSub,
			p.direct,
			participantPeerIDs,
			selfPartyID,
			oldAllPartyIDs,
			newAllPartyIDs,
			oldKeyInfo.Threshold,
			newThreshold,
			p.KVstore,
			p.keyinfoStore,
			resultQueue,
			p.identityStore,
			newPeerIDs,
			isNewPeer,
			oldKeyInfo.Version,
		), nil

	default:
		return nil, fmt.Errorf("unsupported session type: %v", sessionType)
	}
}

func ComposeReadyKey(nodeID string) string {
	return fmt.Sprintf("ready/%s", nodeID)
}

func (p *Node) Close() {
	err := p.peerRegistry.Resign()
	if err != nil {
		logger.Error("Resign failed", err)
	}
}

func (p *Node) generatePreParams() []*keygen.LocalPreParams {
	start := time.Now()
	// Try to load from storage
	preParams := make([]*keygen.LocalPreParams, 2)
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("pre_params_%d", i)
		val, err := p.KVstore.Get(key)
		if err == nil && val != nil {
			preParams[i] = &keygen.LocalPreParams{}
			err = json.Unmarshal(val, preParams[i])
			if err != nil {
				logger.Fatal("Unmarshal pre params failed", err)
			}
			continue
		}
		// Not found, generate and save
		params, err := keygen.GeneratePreParams(5 * time.Minute)
		if err != nil {
			logger.Fatal("Generate pre params failed", err)
		}
		bytes, err := json.Marshal(params)
		if err != nil {
			logger.Fatal("Marshal pre params failed", err)
		}
		err = p.KVstore.Put(key, bytes)
		if err != nil {
			logger.Fatal("Save pre params failed", err)
		}
		preParams[i] = params
	}
	logger.Info("Generate pre params successfully!", "elapsed", time.Since(start).Milliseconds())
	return preParams
}

func (p *Node) getVersion(sessionType core.SessionType, walletID string) int {
	var composeKey string
	switch sessionType {
	case core.SessionTypeECDSA:
		composeKey = fmt.Sprintf("ecdsa:%s", walletID)
	case core.SessionTypeEDDSA:
		composeKey = fmt.Sprintf("eddsa:%s", walletID)
	default:
		logger.Fatal("unknown session type", errors.New("unknown session type"))
	}
	keyinfo, err := p.keyinfoStore.Get(composeKey)
	if err != nil {
		logger.Error("Get keyinfo failed", err, "walletID", walletID)
		return DefaultVersion
	}
	return keyinfo.Version
}

func sessionKeyPrefix(sessionType core.SessionType) (string, error) {
	switch sessionType {
	case core.SessionTypeECDSA:
		return "ecdsa", nil
	case core.SessionTypeEDDSA:
		return "eddsa", nil
	default:
		return "", fmt.Errorf("unsupported session type: %v", sessionType)
	}
}

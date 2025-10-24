package file

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"

	"filippo.io/age"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"golang.org/x/term"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/encryption"
	"github.com/fystack/mpcium/pkg/filesystem"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/node"
	"github.com/fystack/mpcium/pkg/security"
	"github.com/fystack/mpcium/pkg/types"
)

type initiatorKey struct {
	Algorithm types.EventInitiatorKeyType
	Ed25519   []byte
	P256      *ecdsa.PublicKey
}

type fileStore struct {
	identityDir     string
	currentNodeName string
	publicKeys      map[string][]byte
	mu              sync.RWMutex
	privateKey      []byte
	initiatorKey    *initiatorKey
	symmetricKeys   map[string][]byte
}

func NewFileStore(identityDir, nodeName string, decrypt bool, agePasswordFile string) (node.IdentityStore, error) {
	if err := os.MkdirAll(identityDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create identity directory: %w", err)
	}

	privateKeyHex, err := loadPrivateKey(identityDir, nodeName, decrypt, agePasswordFile)
	if err != nil {
		return nil, err
	}

	privateKey, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key format: %w", err)
	}

	initiatorKey, err := loadInitiatorKeys()
	if err != nil {
		return nil, err
	}

	peersData, err := os.ReadFile("peers.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read peers.json: %w", err)
	}

	peers := make(map[string]string)
	if err := json.Unmarshal(peersData, &peers); err != nil {
		return nil, fmt.Errorf("failed to parse peers.json: %w", err)
	}

	store := &fileStore{
		identityDir:     identityDir,
		currentNodeName: nodeName,
		publicKeys:      make(map[string][]byte),
		privateKey:      privateKey,
		initiatorKey:    initiatorKey,
		symmetricKeys:   make(map[string][]byte),
	}

	for peerName, peerID := range peers {
		identityFileName := fmt.Sprintf("%s_identity.json", peerName)
		identityFilePath, err := filesystem.SafePath(identityDir, identityFileName)
		if err != nil {
			return nil, fmt.Errorf("invalid identity file path for node %s: %w", peerName, err)
		}

		data, err := os.ReadFile(identityFilePath)
		if err != nil {
			return nil, fmt.Errorf("missing identity file for node %s (%s): %w", peerName, peerID, err)
		}

		var identity node.NodeIdentity
		if err := json.Unmarshal(data, &identity); err != nil {
			return nil, fmt.Errorf("failed to parse identity file for node %s: %w", peerName, err)
		}

		if identity.NodeID != peerID {
			return nil, fmt.Errorf("node ID mismatch for %s: %s in peers.json vs %s in identity file", peerName, peerID, identity.NodeID)
		}

		key, err := hex.DecodeString(identity.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("invalid public key format for node %s: %w", peerName, err)
		}

		store.publicKeys[identity.NodeID] = key
	}

	return store, nil
}

func loadInitiatorKeys() (*initiatorKey, error) {
	algorithm := config.EventInitiatorAlgorithm()
	if algorithm == "" {
		algorithm = string(types.KeyTypeEd25519)
	}

	if !slices.Contains([]string{string(types.EventInitiatorKeyTypeEd25519), string(types.EventInitiatorKeyTypeP256)}, algorithm) {
		return nil, fmt.Errorf("invalid algorithm: %s. Must be ed25519 or p256", algorithm)
	}

	switch algorithm {
	case string(types.EventInitiatorKeyTypeEd25519):
		key, err := loadEd25519InitiatorKey()
		if err != nil {
			return nil, err
		}
		logger.Info("Loaded Ed25519 initiator public key")
		return &initiatorKey{Algorithm: types.EventInitiatorKeyTypeEd25519, Ed25519: key}, nil
	case string(types.EventInitiatorKeyTypeP256):
		key, err := loadP256InitiatorKey()
		if err != nil {
			return nil, err
		}
		logger.Info("Loaded P-256 initiator public key")
		return &initiatorKey{Algorithm: types.EventInitiatorKeyTypeP256, P256: key}, nil
	}
	return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
}

func loadEd25519InitiatorKey() ([]byte, error) {
	pubKeyHex := config.EventInitiatorPubKey()
	if pubKeyHex == "" {
		return nil, fmt.Errorf("event_initiator_pubkey not found in config")
	}

	key, err := encryption.ParseEd25519PublicKeyFromHex(pubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode event_initiator_pubkey as hex: %w", err)
	}
	return key, nil
}

func loadP256InitiatorKey() (*ecdsa.PublicKey, error) {
	pubKeyHex := config.EventInitiatorPubKey()
	if pubKeyHex == "" {
		return nil, fmt.Errorf("event_initiator_pubkey not found in config")
	}

	if key, err := encryption.ParseP256PublicKeyFromHex(pubKeyHex); err == nil {
		return key, nil
	}

	key, err := encryption.ParseP256PublicKeyFromBase64(pubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode event_initiator_pubkey as hex or base64: %w", err)
	}

	return key, nil
}

func (s *fileStore) SetSymmetricKey(peerID string, key []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.symmetricKeys[peerID] = key
}

func (s *fileStore) GetSymmetricKey(peerID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if key, exists := s.symmetricKeys[peerID]; exists {
		return key, nil
	}

	return nil, fmt.Errorf("symmetric key not found for node ID: %s", peerID)
}

func (s *fileStore) RemoveSymmetricKey(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.symmetricKeys, peerID)
}

func (s *fileStore) GetSymmetricKeyCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.symmetricKeys)
}

func (s *fileStore) CheckSymmetricKeyComplete(desired int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.symmetricKeys) == desired
}

func (s *fileStore) GetPublicKey(nodeID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if key, exists := s.publicKeys[nodeID]; exists {
		return key, nil
	}

	return nil, fmt.Errorf("public key not found for node ID: %s", nodeID)
}

func (s *fileStore) SignMessage(msg *types.TssMessage) ([]byte, error) {
	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message for signing: %w", err)
	}

	signature := ed25519.Sign(s.privateKey, msgBytes)
	return signature, nil
}

func (s *fileStore) VerifyMessage(msg *types.TssMessage) error {
	if msg.Signature == nil {
		return fmt.Errorf("message has no signature")
	}

	senderNodeID := partyIDToNodeID(msg.From)

	publicKey, err := s.GetPublicKey(senderNodeID)
	if err != nil {
		return fmt.Errorf("failed to get sender's public key: %w", err)
	}

	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return fmt.Errorf("failed to marshal message for verification: %w", err)
	}

	if !ed25519.Verify(publicKey, msgBytes, msg.Signature) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

func (s *fileStore) EncryptMessage(plaintext []byte, peerID string) ([]byte, error) {
	key, err := s.GetSymmetricKey(peerID)
	if err != nil {
		return nil, err
	}

	if key == nil {
		return nil, fmt.Errorf("no symmetric key for peer %s", peerID)
	}

	return encryption.EncryptAESGCMWithNonceEmbed(plaintext, key)
}

func (s *fileStore) DecryptMessage(cipher []byte, peerID string) ([]byte, error) {
	key, err := s.GetSymmetricKey(peerID)
	if err != nil {
		return nil, err
	}

	if key == nil {
		return nil, fmt.Errorf("no symmetric key for peer %s", peerID)
	}

	return encryption.DecryptAESGCMWithNonceEmbed(cipher, key)
}

func (s *fileStore) SignEcdhMessage(msg *types.ECDHMessage) ([]byte, error) {
	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message for signing: %w", err)
	}

	signature := ed25519.Sign(s.privateKey, msgBytes)
	return signature, nil
}

func (s *fileStore) VerifySignature(msg *types.ECDHMessage) error {
	if msg.Signature == nil {
		return fmt.Errorf("ECDH message has no signature")
	}

	senderPk, err := s.GetPublicKey(msg.From)
	if err != nil {
		return fmt.Errorf("failed to get sender's public key: %w", err)
	}

	msgBytes, err := msg.MarshalForSigning()
	if err != nil {
		return fmt.Errorf("failed to marshal message for verification: %w", err)
	}

	if !ed25519.Verify(senderPk, msgBytes, msg.Signature) {
		return fmt.Errorf("invalid signature from %s with public key %s", msg.From, hex.EncodeToString(senderPk))
	}

	return nil
}

func (s *fileStore) VerifyInitiatorMessage(msg types.InitiatorMessage) error {
	switch s.initiatorKey.Algorithm {
	case types.EventInitiatorKeyTypeEd25519:
		return s.verifyEd25519(msg)
	case types.EventInitiatorKeyTypeP256:
		return s.verifyP256(msg)
	default:
		return fmt.Errorf("unsupported algorithm: %s", s.initiatorKey.Algorithm)
	}
}

func (s *fileStore) verifyEd25519(msg types.InitiatorMessage) error {
	msgBytes, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("failed to get raw message data: %w", err)
	}
	signature := msg.Sig()
	if len(signature) == 0 {
		return errors.New("signature is empty")
	}

	if !ed25519.Verify(s.initiatorKey.Ed25519, msgBytes, signature) {
		return fmt.Errorf("invalid signature from initiator")
	}
	return nil
}

func (s *fileStore) verifyP256(msg types.InitiatorMessage) error {
	msgBytes, err := msg.Raw()
	if err != nil {
		return fmt.Errorf("failed to get raw message data: %w", err)
	}
	signature := msg.Sig()

	if s.initiatorKey.P256 == nil {
		return fmt.Errorf("initiator public key for secp256r1 is not set")
	}

	return encryption.VerifyP256Signature(s.initiatorKey.P256, msgBytes, signature)
}

func partyIDToNodeID(partyID *tss.PartyID) string {
	return strings.Split(string(partyID.KeyInt().Bytes()), ":")[0]
}

func loadPrivateKey(identityDir, nodeName string, decrypt bool, agePasswordFile string) (string, error) {
	encryptedKeyFileName := fmt.Sprintf("%s_private.key.age", nodeName)
	unencryptedKeyFileName := fmt.Sprintf("%s_private.key", nodeName)

	encryptedKeyPath, err := filesystem.SafePath(identityDir, encryptedKeyFileName)
	if err != nil {
		return "", fmt.Errorf("invalid encrypted key path for node %s: %w", nodeName, err)
	}

	unencryptedKeyPath, err := filesystem.SafePath(identityDir, unencryptedKeyFileName)
	if err != nil {
		return "", fmt.Errorf("invalid unencrypted key path for node %s: %w", nodeName, err)
	}

	if decrypt {
		if _, err := os.Stat(encryptedKeyPath); err != nil {
			return "", fmt.Errorf("failed to check encrypted private key for node %s at %s: %w", nodeName, encryptedKeyPath, err)
		}

		logger.Infof("Using age-encrypted private key for %s", nodeName)

		encryptedFile, err := os.Open(encryptedKeyPath)
		if err != nil {
			return "", fmt.Errorf("failed to open encrypted key file: %w", err)
		}
		defer encryptedFile.Close()

		var passphrase string
		if agePasswordFile != "" {
			data, err := os.ReadFile(agePasswordFile)
			if err != nil {
				return "", fmt.Errorf("failed to read age key file %s: %w", agePasswordFile, err)
			}
			passphrase = strings.TrimSpace(string(data))
			security.ZeroBytes(data)
			logger.Infof("Using passphrase from from file: %s to decrypt node private key", agePasswordFile)
		} else {
			fmt.Print("Enter passphrase to decrypt private key: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return "", fmt.Errorf("failed to read passphrase: %w", err)
			}
			passphrase = string(bytePassword)
			security.ZeroBytes(bytePassword)
		}

		identity, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return "", fmt.Errorf("failed to create identity for decryption: %w", err)
		}

		decrypter, err := age.Decrypt(encryptedFile, identity)
		if err != nil {
			return "", fmt.Errorf("failed to decrypt private key: %w", err)
		}

		decryptedData, err := io.ReadAll(decrypter)
		if err != nil {
			return "", fmt.Errorf("failed to read decrypted key: %w", err)
		}

		security.ZeroString(&passphrase)
		return string(decryptedData), nil
	}

	if _, err := os.Stat(unencryptedKeyPath); err != nil {
		return "", fmt.Errorf("no unencrypted private key found for node %s", nodeName)
	}

	privateKeyData, err := os.ReadFile(unencryptedKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read private key file: %w", err)
	}
	return string(privateKeyData), nil
}

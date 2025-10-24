package keystore

import (
	"encoding/json"
	"fmt"

	"github.com/fystack/mpcium/pkg/infra"
	"github.com/fystack/mpcium/pkg/node"
	"github.com/hashicorp/consul/api"
)

type consulStore struct {
	consulKV infra.ConsulKV
}

var _ node.KeyStore = &consulStore{}

func NewConsulStore(consulKV infra.ConsulKV) node.KeyStore {
	return &consulStore{consulKV: consulKV}
}

func (s *consulStore) Get(walletID string) (*node.KeyInfo, error) {
	pair, _, err := s.consulKV.Get(s.composeKey(walletID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get key info: %w", err)
	}
	if pair == nil {
		return nil, fmt.Errorf("key info not found")
	}

	info := &node.KeyInfo{}
	if err := json.Unmarshal(pair.Value, info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal key info: %w", err)
	}

	return info, nil
}

func (s *consulStore) Save(walletID string, info *node.KeyInfo) error {
	bytes, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal key info: %w", err)
	}

	pair := &api.KVPair{Key: s.composeKey(walletID), Value: bytes}
	if _, err := s.consulKV.Put(pair, nil); err != nil {
		return fmt.Errorf("failed to save key info: %w", err)
	}

	return nil
}

func (s *consulStore) composeKey(walletID string) string {
	return fmt.Sprintf("threshold_keyinfo/%s", walletID)
}

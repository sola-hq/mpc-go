package main

import (
	"fmt"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/hashicorp/consul/api"
)

// NewConsulClient creates a new Consul client
func NewConsulClient(addr string) *api.Client {
	// Create a new Consul client
	consulConfig := api.DefaultConfig()
	consulConfig.Address = addr
	consulClient, err := api.NewClient(consulConfig)
	if err != nil {
		logger.Fatal("Failed to create consul client", err)
	}
	logger.Info("Connected to consul!")
	return consulClient
}

// LoadPeersFromConsul loads peers from Consul
func LoadPeersFromConsul(consulClient *api.Client) []config.Peer { // Create a Consul Key-Value store client
	kv := consulClient.KV()
	peers, err := config.LoadPeersFromConsul(kv, "mpc_peers/")
	if err != nil {
		logger.Fatal("Failed to load peers from Consul", err)
	}
	logger.Info("Loaded peers from consul", "peers", peers)

	return peers
}

func GetPeerIDs(peers []config.Peer) []string {
	var peersIDs []string
	for _, peer := range peers {
		peersIDs = append(peersIDs, peer.ID)
	}
	return peersIDs
}

// Given node name, loop through peers and find the matching ID
func GetIDFromName(name string, peers []config.Peer) string {
	// Get nodeID from node name
	nodeID := config.GetNodeID(name, peers)
	if nodeID == "" {
		logger.Fatal("Empty Node ID", fmt.Errorf("node ID not found for name %s", name))
	}

	return nodeID
}

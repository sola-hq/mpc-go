package mpc

// PeerRegistry defines the coordination surface MPC nodes depend on to track
// peer readiness and lifecycle events.
type PeerRegistry interface {
	Ready() error
	ArePeersReady() bool
	AreMajorityReady() bool
	WatchPeersReady()
	Resign() error
	GetReadyPeersCount() int64
	GetReadyPeersCountExcludeSelf() int64
	GetReadyPeersIncludeSelf() []string
	GetTotalPeersCount() int64

	OnPeerConnected(callback func(peerID string))
	OnPeerDisconnected(callback func(peerID string))
	OnPeerReConnected(callback func(peerID string))
}

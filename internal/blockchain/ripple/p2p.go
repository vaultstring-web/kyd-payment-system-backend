package ripple

import (
	"context"
)

type P2PNode struct {
	NodeID       string
	Host         string
	Port         int
	Peers        map[string]*P2PNode
	MessageQueue chan map[string]interface{}
	IsRunning    bool
}

func NewP2PNode(nodeID string) *P2PNode {
	return &P2PNode{
		NodeID:       nodeID,
		Host:         "127.0.0.1",
		Port:         8000,
		Peers:        make(map[string]*P2PNode),
		MessageQueue: make(chan map[string]interface{}, 1024),
	}
}

func (n *P2PNode) Start(ctx context.Context) {
	if n == nil {
		return
	}
	if n.IsRunning {
		return
	}
	n.IsRunning = true
	go func() {
		for {
			select {
			case <-ctx.Done():
				n.IsRunning = false
				return
			case msg := <-n.MessageQueue:
				n.handleMessage(msg)
			}
		}
	}()
}

func (n *P2PNode) Stop() {
	if n == nil {
		return
	}
	n.IsRunning = false
}

func (n *P2PNode) ConnectPeer(peer *P2PNode) bool {
	if n == nil || peer == nil {
		return false
	}
	n.Peers[peer.NodeID] = peer
	return true
}

func (n *P2PNode) BroadcastMessage(message map[string]interface{}) {
	if n == nil {
		return
	}
	for _, peer := range n.Peers {
		select {
		case peer.MessageQueue <- message:
		default:
		}
	}
}

func (n *P2PNode) handleMessage(message map[string]interface{}) {
	_ = message
}


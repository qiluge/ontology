/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */

package netserver

import (
	"fmt"
	"testing"
	"time"

	common2 "github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/p2pserver/common"
	"github.com/ontio/ontology/p2pserver/message/types"
	"github.com/ontio/ontology/p2pserver/peer"
)

func init() {
	log.InitLog(log.InfoLog, log.Stdout)
	fmt.Println("Start test the netserver...")
}

func creatPeers(cnt uint16) []*peer.Peer {
	np := []*peer.Peer{}
	var syncport uint16
	var id uint64
	for i := uint16(0); i < cnt; i++ {
		syncport = 20224 + i
		id = 0x7533345 + uint64(i)
		heights := make(map[common2.ShardID]*types.HeightInfo)
		heights[common2.RootShardID] = &types.HeightInfo{
			Height:  uint32(434923 + uint32(i)),
			MsgHash: common2.Uint256{1, 2, 3},
		}
		p := peer.NewPeer()
		p.UpdateInfo(time.Now(), 2, 3, syncport, id, 0, heights, "1.5.2")
		p.SetState(4)
		p.SetHttpInfoState(true)
		p.Link.SetAddr("127.0.0.1:10338")
		np = append(np, p)
	}
	return np

}
func TestNewNetServer(t *testing.T) {
	shardId := common2.NewShardIDUnchecked(10)
	server := NewNetServer(shardId)
	server.Start()
	defer server.Halt()

	heights := make(map[common2.ShardID]*types.HeightInfo)
	heights[common2.RootShardID] = &types.HeightInfo{
		Height:  1000,
		MsgHash: common2.Uint256{1, 2, 3},
	}
	server.SetHeight(heights)

	h := server.GetHeight()
	if h[common2.RootShardID].Height != 1000 {
		t.Error("TestNewNetServer set server height error")
	}

	if server.GetRelay() != true {
		t.Error("TestNewNetServer server relay state error", server.GetRelay())
	}
	if server.GetServices() != 1 {
		t.Error("TestNewNetServer server service state error", server.GetServices())
	}
	if server.GetVersion() != common.PROTOCOL_VERSION {
		t.Error("TestNewNetServer server version error", server.GetVersion())
	}
	if server.GetPort() != 20338 {
		t.Error("TestNewNetServer sync port error", server.GetPort())
	}

	fmt.Printf("lastest server time is %s\n", time.Unix(server.GetTime()/1e9, 0).String())
}

func TestNetServerNbrPeer(t *testing.T) {
	shardId := common2.NewShardIDUnchecked(10)
	server := NewNetServer(shardId)
	server.Start()
	defer server.Halt()

	nm := &peer.NbrPeers{}
	nm.Init()
	np := creatPeers(5)
	for _, v := range np {
		server.AddNbrNode(v)
	}
	if server.GetConnectionCnt() != 5 {
		t.Error("TestNetServerNbrPeer GetConnectionCnt error", server.GetConnectionCnt())
	}
	addrs := server.GetNeighborAddrs()
	if len(addrs) != 5 {
		t.Error("TestNetServerNbrPeer GetNeighborAddrs error")
	}
	if server.NodeEstablished(0x7533345) == false {
		t.Error("TestNetServerNbrPeer NodeEstablished error")
	}
	if server.GetPeer(0x7533345) == nil {
		t.Error("TestNetServerNbrPeer GetPeer error")
	}
	p, ok := server.DelNbrNode(0x7533345)
	if ok != true || p == nil {
		t.Error("TestNetServerNbrPeer DelNbrNode error")
	}
	if len(server.GetNeighbors()) != 4 {
		t.Error("TestNetServerNbrPeer GetNeighbors error")
	}
	sp := &peer.Peer{}
	server.AddPeerAddress("127.0.0.1:10338", sp)
	if server.GetPeerFromAddr("127.0.0.1:10338") != sp {
		t.Error("TestNetServerNbrPeer Get/AddPeerConsAddress error")
	}

}

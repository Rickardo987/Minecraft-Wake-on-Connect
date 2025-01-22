// Modified from https://github.com/go-mc/server
package customServer

import (
	"encoding/json"
	"sync"

	"github.com/google/uuid"

	"github.com/Tnze/go-mc/chat"
	"github.com/Tnze/go-mc/data/packetid"
	"github.com/Tnze/go-mc/net"
	pk "github.com/Tnze/go-mc/net/packet"
)

// ListPingHandler collect server running status info
// which is used to handle client ping and list progress.
type ListPingHandler interface {
	// Name of the server.
	// Vanilla server uses its version name, like "1.19.3".
	Name() string

	// The Protocol number.
	// Usually implemented as returning the protocol number the server currently used.
	// If the server supports multiple protocols, should be implemented as returning clientProtocol
	Protocol(clientProtocol int32) int

	// Description also called MOTD, Message Of The Day.
	Description() *chat.Message

	// FavIcon should be a PNG image that is Base64 encoded
	// (without newlines: \n, new lines no longer work since 1.13)
	// and prepended with "data:image/png;base64,".
	//
	// This method can return empty string if no icon is set.
	FavIcon() string

	UpdateDetails(name string, protocol int, description chat.Message, favicon string)
}

type PlayerSample struct {
	Name string    `json:"name"`
	ID   uuid.UUID `json:"id"`
}

func (s *Server) acceptListPing(conn *net.Conn, clientProtocol int32) {
	var p pk.Packet
	for i := 0; i < 2; i++ { // Ping or List. Only allow check twice
		err := conn.ReadPacket(&p)
		if err != nil {
			return
		}

		switch p.ID {
		case packetid.StatusResponse: // List
			var resp []byte
			resp, err = s.listResp(clientProtocol)
			if err != nil {
				break
			}
			err = conn.WritePacket(pk.Marshal(0x00, pk.String(resp)))
		case packetid.StatusPongResponse: // Ping
			err = conn.WritePacket(p)
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) listResp(clientProtocol int32) ([]byte, error) {
	var list struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int            `json:"max"`
			Online int            `json:"online"`
			Sample []PlayerSample `json:"sample"`
		} `json:"players"`
		Description *chat.Message `json:"description"`
		FavIcon     string        `json:"favicon,omitempty"`
	}

	list.Version.Name = s.Name()
	list.Version.Protocol = s.Protocol(clientProtocol)
	list.Players.Max = 0
	list.Players.Online = 0
	list.Players.Sample = nil
	list.Description = s.Description()
	list.FavIcon = s.FavIcon()

	return json.Marshal(list)
}

// PingInfo implements ListPingHandler.
type PingInfo struct {
	name        string
	protocol    int
	description chat.Message
	favicon     string

	infoLock sync.RWMutex
}

// NewPingInfo crate a new PingInfo, the icon can be nil.
// Panic if icon's size is not 64x64.
func NewPingInfo(name string, protocol int, motd chat.Message) (p *PingInfo) {
	var favIcon string
	p = &PingInfo{
		name:        name,
		protocol:    protocol,
		description: motd,
		favicon:     favIcon,
	}
	return
}

func (p *PingInfo) UpdateDetails(name string, protocol int, description chat.Message, favicon string) {
	p.infoLock.Lock()
	defer p.infoLock.Unlock()
	p.name = name
	p.protocol = protocol
	p.description = description
	p.favicon = favicon
}

func (p *PingInfo) Name() string {
	p.infoLock.RLock()
	defer p.infoLock.RUnlock()
	return p.name
}

func (p *PingInfo) Protocol(int32) int {
	p.infoLock.RLock()
	defer p.infoLock.RUnlock()
	return p.protocol
}

func (p *PingInfo) FavIcon() string {
	p.infoLock.RLock()
	defer p.infoLock.RUnlock()
	return p.favicon
}

func (p *PingInfo) Description() *chat.Message {
	p.infoLock.RLock()
	defer p.infoLock.RUnlock()
	return &p.description
}

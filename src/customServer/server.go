package customServer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"mc-docker-autostart/docker"

	"github.com/Tnze/go-mc/chat"
	"github.com/Tnze/go-mc/data/packetid"
	mcnet "github.com/Tnze/go-mc/net"
	pk "github.com/Tnze/go-mc/net/packet"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// AwaitDial tries to connect to a server repeatedly until it succeeds or the context expires.
func AwaitDial(endpoint string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	retryDelay := time.Millisecond * 250 // Somewhat aggressive, we want players connected quickly
	i := 0
	start := time.Now()

	for {
		if i > 1000 { // If i is too big
			return nil, errors.New("AwaitDial: tried to connect too many times")
		}
		select {
		case <-ctx.Done():
			log.Debug().Msg("AwaitDial: Failed.")
			return nil, ctx.Err() // Context expired or canceled
		default:
			i++
			// Attempt to connect
			conn, err := net.DialTimeout("tcp", endpoint, retryDelay)
			if i == 2 {
				log.Debug().Msg("AwaitDial: Taking multiple attempts to connect...")
			}
			if err != nil {
				if netErr, ok := err.(*net.OpError); ok {
					e := netErr.Err.Error()
					if strings.Contains(e, "i/o timeout") || strings.Contains(e, "connection refused") {
						time.Sleep(retryDelay)
						continue
					}
				}
				// Return for non-retryable errors
				return nil, err
			}
			if i != 1 {
				log.Debug().Int("tries", i).Float64("timeTakenSeconds", time.Since(start).Seconds()).Msg("AwaitDial: Connected.")
			}
			return conn, nil
		}
	}
}

type Server struct {
	ListPingHandler
	DockerHandle *docker.DockerManager
	DockerCtx    *context.Context
}

func (s *Server) ListenMC(addr string, downstreamMcEndpoint string) error {
	listener, err := mcnet.ListenMC(addr)
	if err != nil {
		return err
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.AcceptConnMC(&conn, downstreamMcEndpoint)
	}
}

func (s *Server) AcceptConnMC(conn *mcnet.Conn, downstreamMcEndpoint string) {
	defer conn.Close()
	var (
		handshakePacket pk.Packet

		protocol, intention pk.VarInt
		_serverAddress      pk.String        // ignored
		_serverPort         pk.UnsignedShort // ignored
	)

	// receive handshake packet
	err := conn.ReadPacket(&handshakePacket)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to read handshake packet.")
		return
	}

	// decode handshake packet
	if handshakePacket.Scan(&protocol, &_serverAddress, &_serverPort, &intention) != nil {
		log.Warn().Err(err).Msg("Failed to decode handshake packet.")
		return
	}

	s.DockerHandle.UpdateContainerInfo(*s.DockerCtx)
	switch intention {
	case 0x01: // list ping
		log.Debug().Str("addr", conn.Socket.RemoteAddr().String()).Msg("Got ping request.")

		// If server is running, proxy the request to the downstream
		if s.DockerHandle.GetStatus() == "running" {
			err := proxyConn(conn, downstreamMcEndpoint, []pk.Packet{handshakePacket})
			if err != nil {
				log.Warn().Err(err).Msg("Error proxying.")
				return
			}
			return
		}

		// Otherwise, use the custom handler that shows 0/0 users
		s.acceptListPing(conn, (int32)(protocol))
	case 0x02: // login
		var loginStartPacket pk.Packet
		var name pk.String
		var id pk.UUID
		err = conn.ReadPacket(&loginStartPacket)
		if err != nil {
			log.Warn().Err(err).Msg("Error reading packet.")
			return
		}
		if loginStartPacket.ID != packetid.LoginStart {
			err = wrongPacketErr{expect: packetid.LoginStart, get: loginStartPacket.ID}
			log.Warn().Err(err).Msg("Unexpected packet.")
			return
		}

		err = loginStartPacket.Scan(
			(*pk.String)(&name), // decode username as pk.String
			(*pk.UUID)(&id),
		)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to decode client loginPacket.")
			return
		}

		log.Debug().Str("name", (string)(name)).Str("uuid", (uuid.UUID)(id).String()).Msg("Player connected.")

		if s.DockerHandle.GetStatus() != "running" {
			if err := s.DockerHandle.Start(*s.DockerCtx); err != nil {
				msg := chat.Message{Text: "Remote Server Error:", Color: "red"}.Append(chat.Message{Text: " Failed to start server.", Color: "white"})
				conn.WritePacket(pk.Marshal(
					packetid.LoginDisconnect, //0x00
					msg,
				))
				log.Error().Err(err).Msg("Failed to start minecraft container.")
				return
			}
		}

		err := proxyConn(conn, downstreamMcEndpoint, []pk.Packet{handshakePacket, loginStartPacket})
		if err != nil {
			log.Warn().Err(err).Msg("Error proxying.")
			return
		}
		return

		// otherwise, if it takes too long to start, KICK THE PLAYER
		// send login disconnect

		/*msg := chat.Message{Text: "goodbye", Color: "blue"}.Append(chat.Message{Text: " asshole", Color: "red"})
		conn.WritePacket(pk.Marshal(
			packetid.LoginDisconnect, //0x00
			msg,
		))
		return*/
	default:
		err = wrongPacketErr{expect: 0x00, get: (int32)(intention)} // not technically correct, expect 0x01 or 0x02
		log.Warn().Err(err).Msg("Unexpected packet.")
		log.Warn()
		return
	}
}

// caller closes src
func proxyConn(client *mcnet.Conn, dstEndpoint string, initialClientPackets []pk.Packet) error {
	server, err := AwaitDial(dstEndpoint, time.Second*25)
	if err != nil {
		msg := chat.Message{Text: "Server is starting. Please reconnect in a moment.\n\nIf the server never starts, there may be a server issue.", Color: "yellow"}
		client.WritePacket(pk.Marshal(
			packetid.LoginDisconnect, //0x00
			msg,
		))
		return nil
	}
	defer server.Close()

	bits := make(chan int64)

	go func() { // Start proxy for client to server
		w, err := io.Copy(server, client)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to proxy Server->Client")
		}
		bits <- w
	}()

	go func() { // Start proxy for server to client
		w, _ := io.Copy(client, server)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to proxy Client->Server")
		}
		bits <- w
	}()

	for _, p := range initialClientPackets {
		err = p.Pack(server, -1) // Forward previously recieved packet, compression does not seem to work
		if err != nil {
			log.Warn().Err(err).Msg("Error writing handshakePacket.")
			return err
		}
	}

	// Debug log bytes, but also ensures the above go threads are stopped
	written := (int64)(0)
	written += <-bits
	written += <-bits

	log.Debug().Str("addr", client.Socket.RemoteAddr().String()).Int64("totalRecievedAndSent", written).Msg("Done proxying.")

	return nil
}

type wrongPacketErr struct {
	expect, get int32
}

func (w wrongPacketErr) Error() string {
	return fmt.Sprintf("wrong packet id: expect %#02X, get %#02X", w.expect, w.get)
}

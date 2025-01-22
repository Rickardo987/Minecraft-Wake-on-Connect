// This file is part of go-mc/server project.
// Copyright (C) 2023.  Tnze
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Tnze/go-mc/chat"
	"github.com/caarlos0/env/v6"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"mc-docker-autostart/customServer"
	"mc-docker-autostart/docker"
	"mc-docker-autostart/mcping"

	"net/http"
	_ "net/http/pprof"
)

var VERSION = "1.0.0"

type ProgramConfig struct {
	ListenAddress          string `env:"LISTEN_ADDRESS" envDefault:":25565"`
	MinecraftContainerName string `env:"MINECRAFT_CONTAINER_NAME" envDefault:"minecraft"`
	MinecraftEndpoint      string `env:"MINECRAFT_ENDPOINT" envDefault:"172.25.0.5:25565"`

	Debug bool `env:"ENABLE_DEBUG" envDefault:"false"`

	MOTD                   string `env:"STARTUP_MOTD" envDefault:"Server is booting..."` // Does not support color for formatting codes
	StartupName            string `env:"STARTUP_NAME" envDefault:"1.21.4"`               // Also used as client version while contacting downstream
	StartupProtocolVersion int    `env:"STARTUP_PROTOCOL_VERSION" envDefault:"769"`      // Also used as client version while contacting downstream
}

func main() {
	go func() {
		err := http.ListenAndServe("localhost:6060", nil)
		if err != nil {
			log.Fatal().Err(err).Msg("Error while stating debug endpoint")
		}
	}()

	// Log human readable error messages instead of the default json messages
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.DateTime})

	log.Info().Msgf("Docker MC Proxy v%v is starting...", VERSION)

	// Load config from enviornment variables
	config := ProgramConfig{}
	if err := env.Parse(&config); err != nil {
		log.Fatal().Err(err).Msg("Error parsing config from environment variables.")
	}
	log.Info().Str("config", fmt.Sprintf("%+v", config)).Msg("Parsed config.")

	if config.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Setup docker handling
	dm, err := docker.NewDockerManager(config.MinecraftContainerName)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create DockerManager.")
	}

	dockerCtx, dockerCtxCancel := context.WithCancel(context.Background())
	defer dockerCtxCancel()

	containerExists, err := dm.UpdateContainerInfo(dockerCtx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to update container info.")
	}

	if containerExists {
		if err := dm.Start(dockerCtx); err != nil {
			log.Error().Err(err).Msg("Failed to start minecraft container.")
		}
	} else {
		log.Info().Str("state", dm.GetStatus()).Msg("Current container status.")
	}

	// Start watching container events in a separate goroutine
	/*go func() {
		if err := dm.WatchContainerState(dockerCtx); err != nil {
			log.Error().Err(err).Msg("Error watching container events.")
		}
	}()*/

	// Setup listening server
	s := customServer.Server{
		ListPingHandler: customServer.NewPingInfo(
			config.StartupName,
			config.StartupProtocolVersion,
			chat.Text(config.MOTD),
		),
		DockerHandle: dm,
		DockerCtx:    &dockerCtx,
	}

	go func() {
		err := s.ListenMC(config.ListenAddress, config.MinecraftEndpoint)
		if err != nil {
			panic(err)
		}
	}()
	log.Info().Msg("Listening for connections!")

	if e := log.Debug(); e.Enabled() {
		go func() {
			state := dm.GetStatus()
			log.Debug().Str("state", state).Msg("Initial container state.")
			for {
				if state != dm.GetStatus() {
					state = dm.GetStatus()
					log.Debug().Str("state", state).Msg("Docker container state change.")
				}
			}
		}()
	}

	// Update with downstream server details
	if s.DockerHandle.GetStatus() == "running" {
		conn, err := customServer.AwaitDial(config.MinecraftEndpoint, time.Second*25)

		if err != nil {
			log.Error().Err(err).Msg("Error connecting to downstream mc server to copy details.")
		} else {
			status, delay, err := mcping.PingAndListConn(conn, config.StartupProtocolVersion)
			conn.Close()
			if err != nil {
				log.Fatal().Err(err).Msg("Error getting downstream mc server details.")
			} else {
				s.UpdateDetails(status.Version.Name, status.Version.Protocol, status.Description, string(status.Favicon))
				log.Info().Float64("pingSeconds", delay.Seconds()).Msg("Updated server details.")
			}
		}
	}

	select {} // wait forever
}

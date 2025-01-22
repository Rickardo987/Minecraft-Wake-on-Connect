# Minecraft-Wake-on-Connect

Minecraft Wake-on-Connect is a proxy application written in go that manages a downstream minecraft server running in docker. The proxy wakes the server when a player connects, saving resources while being completely transparent to the client player.

## Setup
1. Clone this repository:
   ```bash
   git clone https://github.com/Rickardo987/Minecraft-Wake-on-Connect.git
   cd Minecraft-Wake-on-Connect
   ```
2. Run with:
   ```bash
   docker compose up -d
   ```

## Features to add
- Read server whitelist.
- Accept proxy header to respect ip bans through proxy services.

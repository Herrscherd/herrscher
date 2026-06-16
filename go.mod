module github.com/Herrscherd/herrscher

go 1.23

require (
	github.com/Herrscherd/dctl v0.0.0
	github.com/Herrscherd/herrscher-claude-backend v0.0.0
	github.com/Herrscherd/herrscher-contracts v0.0.0
	github.com/Herrscherd/herrscher-discord-gateway v0.0.0
)

require github.com/coder/websocket v1.8.14 // indirect

replace github.com/Herrscherd/dctl => ../dctl

replace github.com/Herrscherd/herrscher-contracts => ../herrscher-contracts

replace github.com/Herrscherd/herrscher-claude-backend => ../herrscher-claude-backend

replace github.com/Herrscherd/herrscher-discord-gateway => ../herrscher-discord-gateway

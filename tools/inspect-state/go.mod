module github.com/AltairaLabs/PromptKit/tools/inspect-state

go 1.25.1

require github.com/AltairaLabs/PromptKit/runtime v0.0.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/redis/go-redis/v9 v9.17.2 // indirect
)

replace github.com/AltairaLabs/PromptKit/runtime => ../../runtime

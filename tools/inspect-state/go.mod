module github.com/AltairaLabs/PromptKit/tools/inspect-state

go 1.25.1

require github.com/AltairaLabs/PromptKit/runtime v0.0.0

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.4 // indirect
	github.com/aws/smithy-go v1.24.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/redis/go-redis/v9 v9.18.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)

replace github.com/AltairaLabs/PromptKit/runtime => ../../runtime

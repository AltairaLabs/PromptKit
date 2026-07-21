module multitrack-ingestion

go 1.26.0

require (
	github.com/AltairaLabs/PromptKit/runtime v1.3.5
	github.com/AltairaLabs/PromptKit/sdk v0.0.0-00010101000000-000000000000
)

replace github.com/AltairaLabs/PromptKit/runtime => ../../../runtime

replace github.com/AltairaLabs/PromptKit/sdk => ../../../sdk

replace github.com/AltairaLabs/PromptKit/pkg => ../../../pkg

replace github.com/AltairaLabs/PromptKit/server/a2a => ../../../server/a2a

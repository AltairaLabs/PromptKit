module github.com/AltairaLabs/PromptKit/tools/schema-gen

go 1.25.1

replace (
	github.com/AltairaLabs/PromptKit/pkg => ../../pkg
	github.com/AltairaLabs/PromptKit/runtime => ../../runtime
	github.com/AltairaLabs/PromptKit/tools/arena => ../arena
)

require (
	github.com/AltairaLabs/PromptKit/pkg v1.2.0
	github.com/invopop/jsonschema v0.13.0
	k8s.io/apimachinery v0.35.1
)

require (
	github.com/AltairaLabs/PromptKit/runtime v1.2.0 // indirect
	github.com/AltairaLabs/PromptKit/tools/arena v1.2.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.4 // indirect
	github.com/aws/smithy-go v1.24.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20180127040702-4e3ac2762d5f // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250910181357-589584f1c912 // indirect
	k8s.io/utils v0.0.0-20251002143259-bc988d571ff4 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
)

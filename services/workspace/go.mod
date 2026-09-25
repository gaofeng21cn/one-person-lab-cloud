module opl-cloud/services/workspace

go 1.26.0

require (
	opl-cloud/services/internal/ownerservice v0.0.0
	opl-cloud/services/internal/ownerstore v0.0.0
)

require (
	ariga.io/atlas v0.36.2-0.20250730182955-2c6300d0a3e1 // indirect
	entgo.io/ent v0.14.6 // indirect
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/bmatcuk/doublestar v1.3.4 // indirect
	github.com/dlclark/regexp2 v1.11.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-openapi/inflect v0.19.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/hcl/v2 v2.18.1 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/zclconf/go-cty v1.14.4 // indirect
	github.com/zclconf/go-cty-yaml v1.1.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apimachinery v0.37.0 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
	opl-cloud/services/internal/postgresmigrate v0.0.0 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.2 // indirect
)

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

replace opl-cloud/services/internal/ownerservice => ../internal/ownerservice

replace opl-cloud/services/internal/ownerstore => ../internal/ownerstore

replace opl-cloud/services/internal/postgresmigrate => ../internal/postgresmigrate

require opl-cloud/apps/console-bff v0.0.0

replace opl-cloud/apps/console-bff => ../../apps/console-bff

require opl-cloud/services/resource-catalog v0.0.0

replace opl-cloud/services/resource-catalog => ../resource-catalog

require opl-cloud/services/gateway-integration v0.0.0

replace opl-cloud/services/gateway-integration => ../gateway-integration

require (
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/capability v0.0.0
	opl-cloud/services/fabric v0.0.0
	opl-cloud/services/ledger v0.0.0
	opl-cloud/services/serve v0.0.0
)

replace opl-cloud/services/fabric => ../fabric

replace opl-cloud/services/ledger => ../ledger

replace opl-cloud/services/capability => ../capability

replace opl-cloud/services/serve => ../serve

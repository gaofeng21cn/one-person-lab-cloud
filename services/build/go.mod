module opl-cloud/services/build

go 1.25.0

require (
	opl-cloud/apps/console-bff v0.0.0-00010101000000-000000000000
	opl-cloud/services/internal/ownerservice v0.0.0
	opl-cloud/services/internal/ownerstore v0.0.0-00010101000000-000000000000
	opl-cloud/services/ledger v0.0.0-00010101000000-000000000000
)

require (
	ariga.io/atlas v0.36.2-0.20250730182955-2c6300d0a3e1 // indirect
	entgo.io/ent v0.14.6 // indirect
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/bmatcuk/doublestar v1.3.4 // indirect
	github.com/dlclark/regexp2 v1.11.0 // indirect
	github.com/go-openapi/inflect v0.19.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/hcl/v2 v2.18.1 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/zclconf/go-cty v1.14.4 // indirect
	github.com/zclconf/go-cty-yaml v1.1.0 // indirect
	golang.org/x/mod v0.38.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/lib/pq v1.12.3
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/capability v0.0.0
	opl-cloud/services/internal/postgresmigrate v0.0.0 // indirect
	opl-cloud/services/runtime-control v0.0.0
)

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

replace opl-cloud/services/internal/ownerservice => ../internal/ownerservice

replace opl-cloud/services/internal/ownerstore => ../internal/ownerstore

replace opl-cloud/services/internal/postgresmigrate => ../internal/postgresmigrate

replace opl-cloud/services/capability => ../capability

replace opl-cloud/services/runtime-control => ../runtime-control

replace opl-cloud/apps/console-bff => ../../apps/console-bff

replace opl-cloud/services/ledger => ../ledger

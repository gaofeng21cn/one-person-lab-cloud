module opl-cloud/services/fabric

go 1.26.0

require (
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cbs v1.3.165
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common v1.3.170
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm v1.3.162
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke v1.3.169
	github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc v1.3.170
	golang.org/x/sys v0.47.0
	k8s.io/apimachinery v0.37.0
	opl-cloud/packages/contracts/go v0.0.0
	opl-cloud/services/internal/postgresmigrate v0.0.0
)

require (
	github.com/agext/levenshtein v1.2.3 // indirect
	github.com/apparentlymart/go-textseg/v15 v15.0.0 // indirect
	github.com/bmatcuk/doublestar v1.3.4 // indirect
	github.com/hashicorp/hcl/v2 v2.18.1 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/zclconf/go-cty-yaml v1.1.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.2 // indirect
)

replace opl-cloud/services/internal/postgresmigrate => ../internal/postgresmigrate

replace opl-cloud/packages/contracts/go => ../../packages/contracts/go

require (
	ariga.io/atlas v0.36.2-0.20250730182955-2c6300d0a3e1 // indirect
	entgo.io/ent v0.14.6
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-openapi/inflect v0.19.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lib/pq v1.12.3
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/zclconf/go-cty v1.14.4 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
)

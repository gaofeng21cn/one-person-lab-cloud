package launch

import (
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// TestActivationRejectsUnprovenEntitlement drives the paid entitlement gate
// directly: every input that is not this exact order's confirmed charge and
// Serve's own confirmed readiness must be refused, so no fabricated or
// foreign evidence can create a paid period or activate the Workspace.
func TestActivationRejectsUnprovenEntitlement(t *testing.T) {
	db := paidOrderDatabase(t)
	service, op, accepted := seedPaidOrder(t, db)
	_, _, paid := paidRuntimeOrder(t)
	charged := mustPaid(t, paid)
	command := activationCommand(op)
	ready := readyRuntime(command)
	stored := orderResult{}
	raw, _ := json.Marshal(stored)
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET status='running',observation_result='confirmed',stage='runtime',result=$2 WHERE id=$1`, op.ID, raw); err != nil {
		t.Fatal(err)
	}
	for name, arrange := range map[string]func(*api.WalletActionReceiptEvidence, *api.RuntimeReadback){
		"foreign obligation": func(e *api.WalletActionReceiptEvidence, _ *api.RuntimeReadback) {
			e.Receipt.OperationId = proto.String("operation-other")
		},
		"foreign workspace": func(e *api.WalletActionReceiptEvidence, _ *api.RuntimeReadback) {
			e.WalletOperation.WorkspaceId = proto.String("workspace-other")
		},
		"unconfirmed charge": func(e *api.WalletActionReceiptEvidence, _ *api.RuntimeReadback) {
			e.WalletOperation.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN
		},
		"receipt is not the charge receipt": func(e *api.WalletActionReceiptEvidence, _ *api.RuntimeReadback) {
			e.WalletOperation.ReceiptId = proto.String("receipt-other")
		},
		"unready runtime": func(_ *api.WalletActionReceiptEvidence, r *api.RuntimeReadback) {
			r.State, r.ProcessReady, r.ApplicationAvailable = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING, false, false
		},
		"readiness without route receipt": func(_ *api.WalletActionReceiptEvidence, r *api.RuntimeReadback) {
			r.ReadinessReceiptId = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			evidence := proto.Clone(charged).(*api.WalletActionReceiptEvidence)
			observed := proto.Clone(ready).(*api.RuntimeReadback)
			arrange(evidence, observed)
			result := orderResult{WalletActionReceipt: wire(evidence)}
			if err := service.activatePaidOrder(t.Context(), op, "lease-token", accepted, &result, observed); err == nil {
				t.Fatal("unproven entitlement evidence was accepted")
			}
			var subscriptions, periods int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscriptions`).Scan(&subscriptions); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscription_periods`).Scan(&periods); err != nil {
				t.Fatal(err)
			}
			if subscriptions != 0 || periods != 0 {
				t.Fatalf("refused activation wrote %d subscriptions and %d periods", subscriptions, periods)
			}
		})
	}
}

// TestActivationRequiresTheAcceptedPeriod proves an accepted quote that cannot
// define a paid period is refused rather than completed with an invented one.
func TestActivationRequiresTheAcceptedPeriod(t *testing.T) {
	db := paidOrderDatabase(t)
	service, op, accepted := seedPaidOrder(t, db)
	_, _, paid := paidRuntimeOrder(t)
	paidEvidence := mustPaid(t, paid)
	valid := paidEvidence.GetWalletOperation()
	command := activationCommand(op)
	ready := readyRuntime(command)
	for name, mutate := range map[string]func(*api.QuoteAcceptance){
		"no period start": func(a *api.QuoteAcceptance) { a.Quote.PeriodStart = nil },
		"no period end":   func(a *api.QuoteAcceptance) { a.Quote.PeriodEnd = nil },
		"inverted period": func(a *api.QuoteAcceptance) {
			a.Quote.PeriodEnd = timestamppb.New(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))
		},
		"no price basis": func(a *api.QuoteAcceptance) { a.Quote.PricePolicyVersionId = "" },
		"zero price": func(a *api.QuoteAcceptance) {
			a.Quote.TotalUsdMicros, a.ResourcePlan.BillingMode = 0, "PREPAID_MONTHLY"
		},
		"no period months": func(a *api.QuoteAcceptance) { a.Quote.PeriodMonths = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			quote := proto.Clone(accepted).(*api.QuoteAcceptance)
			mutate(quote)
			charge := proto.Clone(valid).(*api.WalletOperation)
			charge.AmountUsdMicros = quote.GetQuote().GetTotalUsdMicros()
			evidence := &api.WalletActionReceiptEvidence{Receipt: proto.Clone(paidEvidence.GetReceipt()).(*api.Receipt), QuoteAcceptance: quote, WalletOperation: charge, EvidenceDigest: quote.GetSnapshotDigest()}
			result := orderResult{WalletActionReceipt: wire(evidence)}
			if err := service.activatePaidOrder(t.Context(), op, "lease-token", quote, &result, ready); status.Code(err) != codes.DataLoss {
				t.Fatalf("an accepted quote without a definable paid period was accepted: %v", err)
			}
		})
	}
}

// activationCommand builds the minimal frozen deploy command a readiness
// readback must name; the readiness helpers read its descriptor and identity.
func activationCommand(op ownerstore.Operation) *api.RuntimeDeployCommand {
	artifact := &api.ArtifactReference{Repository: "local.example/application", Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222", Platform: &api.ImagePlatform{Os: api.ImagePlatformOsEnum_IMAGE_PLATFORM_OS_ENUM_LINUX, Architecture: api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_AMD64}}
	return &api.RuntimeDeployCommand{WorkspaceId: op.ResourceID, RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original", ExecutionEpoch: 1, DeploymentDescriptor: &api.DeploymentDescriptor{Artifact: artifact, SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD}, DeploymentDescriptorDigest: "sha256:descriptor", DeploymentDescriptorObjectRef: "descriptor-original"}
}

package controlplane

import (
	"context"
	"errors"
	"os"
	"strings"

	"opl-cloud/services/control-plane/internal/clients"
)

// ProviderAcceptanceLaunchZone preserves the legacy create-only zone configuration
// through the OPL_TENCENT_ZONE environment variable as an explicit external
// Instance/Fabric cutover shim. The Instance owner supplies the value and the Fabric
// Tencent adapter owns its interpretation. Do not add a fallback, a second configuration
// path, or provider-specific validation here; retire this read once the Instance adopts
// a provider-neutral zone supply. ProviderFactsBatch remains readback authority.
func ProviderAcceptanceLaunchZone() string {
	return strings.TrimSpace(os.Getenv("OPL_TENCENT_ZONE"))
}

func (s *Service) Sub2APIBalance(ctx context.Context, userID int64) (clients.Sub2APIBalance, error) {
	if s.sub2API == nil {
		return clients.Sub2APIBalance{}, errors.New("sub2api_unavailable")
	}
	return s.sub2API.Balance(ctx, userID)
}

func (s *Service) ChargeSub2API(ctx context.Context, input clients.Sub2APIChargeInput) (clients.Sub2APICharge, error) {
	if s.sub2API == nil {
		return clients.Sub2APICharge{}, errors.New("sub2api_unavailable")
	}
	return s.sub2API.Charge(ctx, input)
}

func (s *Service) RefundSub2API(ctx context.Context, input clients.Sub2APIRefundInput) (clients.Sub2APIRefund, error) {
	client, ok := s.sub2API.(clients.Sub2APIRefundClient)
	if !ok {
		return clients.Sub2APIRefund{}, errors.New("sub2api_refund_unavailable")
	}
	return client.Refund(ctx, input)
}

func (s *Service) RecordMonthlyReceipt(ctx context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	return s.ledger.RecordReceipt(ctx, input, key)
}

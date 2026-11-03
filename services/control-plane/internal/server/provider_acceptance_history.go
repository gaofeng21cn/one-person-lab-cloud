package server

// These identities are retained only for reading the original verification
// Workspaces. Access and billing readers must still recognize their persisted
// resource bindings after the procurement implementation is removed.
type providerAcceptanceSlot struct {
	ID        string
	AccountID string
	Key       string
}

var providerAcceptanceSlots = map[string]providerAcceptanceSlot{
	"verification-slot-basic-01": {
		ID: "verification-slot-basic-01", AccountID: "acct-verification-slot-basic-01",
		Key: "provider-acceptance:verification-slot-basic-01",
	},
	"verification-slot-pro-01": {
		ID: "verification-slot-pro-01", AccountID: "acct-verification-slot-pro-01",
		Key: "provider-acceptance:verification-slot-pro-01",
	},
}

func providerAcceptanceComputeID(slot providerAcceptanceSlot) string {
	return resourceIDForMutation("ca", slot.AccountID, slot.Key+":compute")
}

func providerAcceptanceStorageID(slot providerAcceptanceSlot) string {
	return resourceIDForMutation("vol", slot.AccountID, slot.Key+":storage")
}

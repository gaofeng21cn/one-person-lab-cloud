package server

import (
	"errors"
	"math"
	"testing"
)

const pilotPriceVersion = "pilot-usd-2026-07-v1"

// acceptedPricePreview re-derives the charge of the accepted price version the
// way the billing history and the accepted Launch readback do: the stored
// price version selects the catalog, and the catalog computes the charge.
func acceptedPricePreview(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	catalog, ok := pricingCatalogByVersion(pricingCatalogVersion)
	if !ok {
		t.Fatal("accepted price version is not registered")
	}
	preview, err := pricingPreviewFromCatalog(catalog, input)
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func assertAcceptedUSDPrice(t *testing.T, quote map[string]any, usdMicros int64) {
	t.Helper()
	if quote["priceVersion"] != pilotPriceVersion || quote["currency"] != "USD" || quote["chargeUsdMicros"] != usdMicros {
		t.Fatalf("accepted USD price = %#v", quote)
	}
	if _, ok := quote["monthlyPriceCnyCents"]; ok {
		t.Fatalf("internal CNY cost leaked into the accepted price: %#v", quote)
	}
}

func TestAcceptedPriceVersionKeepsItsFrozenCatalogFacts(t *testing.T) {
	catalog := defaultPricingCatalog()
	if catalog.Version != pilotPriceVersion || catalog.Currency != "USD" || catalog.WalletCurrency != "USD" || catalog.BillingUnit != "calendar_month" {
		t.Fatalf("accepted catalog identity = %#v", catalog)
	}
	if catalog.StorageBlockGB != 10 || catalog.StorageBlockPriceUSDMicros != 2_580_000 {
		t.Fatalf("accepted storage block = %#v", catalog)
	}
	if len(catalog.Packages) != 2 {
		t.Fatalf("accepted packages = %#v", catalog.Packages)
	}
	for index, expected := range []pricingPackageData{
		{ID: "basic", Name: "Basic", Available: true, ChargeUSDMicros: 50000000},
		{ID: "pro", Name: "Pro", Available: true, ChargeUSDMicros: 214280000},
	} {
		if catalog.Packages[index] != expected {
			t.Fatalf("accepted package %d = %#v", index, catalog.Packages[index])
		}
	}

	// The Console package rows carry identity and availability only. A price is
	// not published by the Control Plane: the Resource Catalog prices new
	// resources.
	rows := packageRows(catalog)
	if len(rows) != 2 {
		t.Fatalf("package rows = %#v", rows)
	}
	for index, packageID := range []string{"basic", "pro"} {
		row, _ := rows[index].(map[string]any)
		if row["id"] != packageID || row["available"] != true || len(row) != 3 || row["name"] == "" {
			t.Fatalf("Console package projection = %#v, want id/name/available only", row)
		}
	}
}

func TestPricingCatalogLookupRequiresExactAcceptedVersion(t *testing.T) {
	catalog, ok := pricingCatalogByVersion(pricingCatalogVersion)
	if !ok || catalog.Version != pricingCatalogVersion {
		t.Fatalf("current pricing catalog lookup = %#v, %v", catalog, ok)
	}
	for _, version := range []string{"", "unknown-price-version", " " + pricingCatalogVersion} {
		if catalog, ok := pricingCatalogByVersion(version); ok {
			t.Fatalf("unknown pricing catalog %q resolved to %#v", version, catalog)
		}
	}
}

func TestPricingPreviewUsesVersionedStorageCatalogFacts(t *testing.T) {
	base := pricingCatalogData{
		Version: "catalog-storage-a", Currency: pricingCurrency, WalletCurrency: pricingWalletCurrency, BillingUnit: pricingBillingUnit,
		StorageBlockGB: 10, StorageBlockPriceUSDMicros: 2_000_000,
		Packages: []pricingPackageData{{ID: "basic", Name: "Basic", Available: true, ChargeUSDMicros: 50_000_000}},
	}
	alternate := base
	alternate.Version = "catalog-storage-b"
	alternate.StorageBlockGB = 20
	alternate.StorageBlockPriceUSDMicros = 7_000_000

	for _, tc := range []struct {
		catalog                pricingCatalogData
		storagePrice, totalUSD int64
	}{
		{catalog: base, storagePrice: 8_000_000, totalUSD: 58_000_000},
		{catalog: alternate, storagePrice: 14_000_000, totalUSD: 64_000_000},
	} {
		storage, err := pricingPreviewFromCatalog(tc.catalog, map[string]any{"resourceType": "storage", "packageId": "basic", "sizeGb": 40})
		if err != nil || storage["priceVersion"] != tc.catalog.Version || storage["chargeUsdMicros"] != tc.storagePrice {
			t.Fatalf("storage quote catalog=%s quote=%#v err=%v", tc.catalog.Version, storage, err)
		}
		workspace, err := workspacePricingPreview(tc.catalog, map[string]any{"packageId": "basic", "sizeGb": 40})
		if err != nil || mapField(workspace, "storage")["chargeUsdMicros"] != tc.storagePrice || workspace["totalChargeUsdMicros"] != tc.totalUSD {
			t.Fatalf("Workspace quote catalog=%s quote=%#v err=%v", tc.catalog.Version, workspace, err)
		}
	}
}

func TestPricingPreviewRejectsInvalidOrOverflowingStorageCatalogFacts(t *testing.T) {
	valid := pricingCatalogData{
		Version: "catalog-storage-valid", Currency: pricingCurrency, WalletCurrency: pricingWalletCurrency, BillingUnit: pricingBillingUnit,
		StorageBlockGB: 10, StorageBlockPriceUSDMicros: 2_000_000,
		Packages: []pricingPackageData{{ID: "basic", Name: "Basic", Available: true, ChargeUSDMicros: 50_000_000}},
	}
	for _, tc := range []struct {
		name    string
		catalog pricingCatalogData
		sizeGB  int64
	}{
		{name: "zero block size", catalog: func() pricingCatalogData { value := valid; value.StorageBlockGB = 0; return value }(), sizeGB: 10},
		{name: "negative block size", catalog: func() pricingCatalogData { value := valid; value.StorageBlockGB = -10; return value }(), sizeGB: 10},
		{name: "zero block price", catalog: func() pricingCatalogData { value := valid; value.StorageBlockPriceUSDMicros = 0; return value }(), sizeGB: 10},
		{name: "negative block price", catalog: func() pricingCatalogData { value := valid; value.StorageBlockPriceUSDMicros = -1; return value }(), sizeGB: 10},
		{name: "charge multiplication overflow", catalog: func() pricingCatalogData {
			value := valid
			value.StorageBlockGB = 1
			value.StorageBlockPriceUSDMicros = math.MaxInt64
			return value
		}(), sizeGB: 2},
		{name: "JSON size precision overflow", catalog: func() pricingCatalogData {
			value := valid
			value.StorageBlockGB = 1
			value.StorageBlockPriceUSDMicros = 1
			return value
		}(), sizeGB: int64(maxJSONSafeInteger) + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if quote, err := pricingPreviewFromCatalog(tc.catalog, map[string]any{"resourceType": "storage", "packageId": "basic", "sizeGb": tc.sizeGB}); err == nil {
				t.Fatalf("invalid storage catalog produced quote=%#v", quote)
			}
		})
	}
}

func TestAcceptedStoragePriceUsesFixedUSDComponents(t *testing.T) {
	for _, tc := range []struct {
		sizeGB    int
		usdMicros int64
	}{{10, 2_580_000}, {100, 25_800_000}} {
		preview := acceptedPricePreview(t, map[string]any{"resourceType": "storage", "packageId": "basic", "sizeGb": tc.sizeGB})
		assertAcceptedUSDPrice(t, preview, tc.usdMicros)
		snapshot := mapField(preview, "priceSnapshot")
		assertAcceptedUSDPrice(t, snapshot, tc.usdMicros)
		if snapshot["sizeGb"] != float64(tc.sizeGB) {
			t.Fatalf("%dGB storage price = %#v", tc.sizeGB, preview)
		}
	}
}

func TestAcceptedProComputeUsesFixedUSDPrice(t *testing.T) {
	preview := acceptedPricePreview(t, map[string]any{"resourceType": "compute", "packageId": "pro"})
	assertAcceptedUSDPrice(t, preview, 214_280_000)
	assertAcceptedUSDPrice(t, mapField(preview, "priceSnapshot"), 214_280_000)
}

func TestWorkspacePricingPreviewAllowsOnlyFrozenPackageStoragePairs(t *testing.T) {
	for _, tc := range []struct {
		packageID, name       string
		sizeGB                int
		compute, storage, sum int64
	}{
		{packageID: "basic", name: "Basic", sizeGB: 10, compute: 50_000_000, storage: 2_580_000, sum: 52_580_000},
		{packageID: "pro", name: "Pro", sizeGB: 100, compute: 214_280_000, storage: 25_800_000, sum: 240_080_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preview := acceptedPricePreview(t, map[string]any{"resourceType": "workspace", "packageId": tc.packageID, "sizeGb": tc.sizeGB})
			if preview["priceVersion"] != pilotPriceVersion || preview["currency"] != "USD" || preview["totalChargeUsdMicros"] != tc.sum {
				t.Fatalf("workspace price = %#v", preview)
			}
			assertAcceptedUSDPrice(t, mapField(preview, "compute"), tc.compute)
			assertAcceptedUSDPrice(t, mapField(preview, "storage"), tc.storage)
		})
	}

	for _, input := range []map[string]any{
		{"resourceType": "workspace", "packageId": "basic", "sizeGb": 11},
		{"resourceType": "workspace", "packageId": "basic", "sizeGb": 0},
		{"resourceType": "workspace", "packageId": ""},
	} {
		if _, err := pricingPreviewFromCatalog(defaultPricingCatalog(), input); !errors.Is(err, errInvalidPricingInput) {
			t.Fatalf("invalid workspace price input %#v error = %v", input, err)
		}
	}
}

func TestAcceptedPricingSnapshotUsesCanonicalAuthority(t *testing.T) {
	preview := acceptedPricePreview(t, map[string]any{"resourceType": "compute", "packageId": "basic"})
	snapshot := mapField(preview, "priceSnapshot")
	if preview["priceVersion"] != pilotPriceVersion || preview["currency"] != "USD" || preview["chargeUsdMicros"] != int64(50_000_000) ||
		snapshot["priceVersion"] != pilotPriceVersion || snapshot["currency"] != "USD" || snapshot["billingUnit"] != "calendar_month" || snapshot["chargeUsdMicros"] != int64(50_000_000) {
		t.Fatalf("accepted pricing snapshot = %#v", preview)
	}
	if preview["pricingVersion"] != pilotPriceVersion {
		t.Fatalf("accepted pricing version projection = %#v", preview)
	}
	for _, value := range []map[string]any{preview, snapshot} {
		if _, ok := value["monthlyPriceCnyCents"]; ok {
			t.Fatalf("Fabric provider cost leaked into the accepted price: %#v", value)
		}
	}
	workspace := acceptedPricePreview(t, map[string]any{"resourceType": "workspace", "packageId": "basic", "sizeGb": 10})
	if _, ok := workspace["totalMonthlyPriceCnyCents"]; ok {
		t.Fatalf("Fabric provider cost total leaked into the accepted Workspace charge: %#v", workspace)
	}
}

func TestWorkspacePricingPreviewRejectsInvalidStorage(t *testing.T) {
	if _, err := pricingPreviewFromCatalog(defaultPricingCatalog(), map[string]any{
		"resourceType": "workspace", "packageId": "basic", "sizeGb": 11,
	}); !errors.Is(err, errInvalidPricingInput) {
		t.Fatalf("error = %v, want invalid pricing input", err)
	}
}

func TestAcceptedPricingRejectsInvalidProducts(t *testing.T) {
	for name, input := range map[string]map[string]any{
		"unknown compute package": {"resourceType": "compute", "packageId": "enterprise"},
		"storage below minimum":   {"resourceType": "storage", "packageId": "basic", "sizeGb": 9},
		"storage partial block":   {"resourceType": "storage", "packageId": "basic", "sizeGb": 15},
		"storage fractional size": {"resourceType": "storage", "packageId": "basic", "sizeGb": 10.5},
		"storage charge overflow": {"resourceType": "storage", "packageId": "basic", "sizeGb": 100_000_000_000_000},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pricingPreviewFromCatalog(defaultPricingCatalog(), input); err == nil {
				t.Fatalf("pricing input should be rejected: %#v", input)
			}
		})
	}
}

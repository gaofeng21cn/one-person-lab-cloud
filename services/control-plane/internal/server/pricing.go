package server

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// This file keeps only the versioned price facts the Control Plane still owns
// as history: the catalog of a known price version and the computation that
// re-derives an accepted charge from it. The live pricing surface
// (`GET /api/pricing/catalog`, `POST /api/pricing/preview`) was withdrawn in
// the R6 slice: the Resource Catalog prices new resources and quotes them,
// and the Control Plane no longer publishes a current price or a preview.
//
// Accepted operations, their receipts and the billing history still carry the
// price version they were charged at, so the decoders below stay the one place
// that turns a stored price version back into the amount the customer was
// charged. They are readers, not a pricing writer: nothing here accepts a
// price from a client, and nothing here decides what a new purchase costs.

const (
	pricingCatalogVersion = "pilot-usd-2026-07-v1"
	pricingCurrency       = "USD"
	pricingWalletCurrency = "USD"
	pricingBillingUnit    = "calendar_month"
)

var errInvalidPricingInput = errors.New("invalid pricing input")

type pricingCatalogData struct {
	Version                    string
	Currency                   string
	WalletCurrency             string
	BillingUnit                string
	StorageBlockGB             int64
	StorageBlockPriceUSDMicros int64
	Packages                   []pricingPackageData
}

type pricingPackageData struct {
	ID              string
	Name            string
	Available       bool
	ChargeUSDMicros int64
}

func defaultPricingCatalog() pricingCatalogData {
	catalog, ok := pricingCatalogByVersion(pricingCatalogVersion)
	if !ok {
		panic("current pricing catalog is not registered")
	}
	return catalog
}

// pricingCatalogByVersion resolves the catalog of one accepted price version.
// An unknown version resolves to nothing: a stored charge is only re-derived
// under the version it was accepted with.
func pricingCatalogByVersion(priceVersion string) (pricingCatalogData, bool) {
	switch priceVersion {
	case pricingCatalogVersion:
		return pricingCatalogData{
			Version: pricingCatalogVersion, Currency: pricingCurrency, WalletCurrency: pricingWalletCurrency,
			BillingUnit: pricingBillingUnit, StorageBlockGB: 10, StorageBlockPriceUSDMicros: 2_580_000,
			Packages: []pricingPackageData{
				{ID: "basic", Name: "Basic", Available: true, ChargeUSDMicros: 50000000},
				{ID: "pro", Name: "Pro", Available: true, ChargeUSDMicros: 214280000},
			},
		}, true
	default:
		return pricingCatalogData{}, false
	}
}

func pricingPreviewFromCatalog(catalog pricingCatalogData, input map[string]any) (map[string]any, error) {
	resourceType := firstNonEmpty(stringField(input, "resourceType", ""), "compute")
	if resourceType == "workspace" {
		return workspacePricingPreview(catalog, input)
	}
	if resourceType != "compute" && resourceType != "storage" {
		return nil, fmt.Errorf("%w: unknown resource type %q", errInvalidPricingInput, resourceType)
	}
	packageID := firstNonEmpty(stringField(input, "packageId", ""), "basic")
	plan, ok := packageByIDFromCatalog(catalog, packageID)
	if !ok {
		return nil, fmt.Errorf("%w: unknown package %q", errInvalidPricingInput, packageID)
	}
	chargeUSDMicros := plan.ChargeUSDMicros
	snapshot := map[string]any{
		"priceVersion": catalog.Version, "pricingVersion": catalog.Version, "packageId": plan.ID, "currency": catalog.Currency,
		"displayCurrency": catalog.Currency,
		"billingUnit":     catalog.BillingUnit,
		"chargeUsdMicros": chargeUSDMicros, "resourceType": resourceType,
	}
	if resourceType == "storage" {
		maxStorageGB, validStorageCatalog := catalogStorageLimitGB(catalog)
		if !validStorageCatalog {
			return nil, fmt.Errorf("%w: invalid storage catalog", errInvalidPricingInput)
		}
		sizeGB := numberField(input, "sizeGb", numberField(input, "sizeGB", float64(catalog.StorageBlockGB)))
		if math.IsNaN(sizeGB) || math.IsInf(sizeGB, 0) || sizeGB < float64(catalog.StorageBlockGB) || sizeGB > maxJSONSafeInteger ||
			sizeGB != math.Trunc(sizeGB) || int64(sizeGB) > maxStorageGB || int64(sizeGB)%catalog.StorageBlockGB != 0 {
			return nil, fmt.Errorf("%w: storage size must be a positive multiple of %dGB", errInvalidPricingInput, catalog.StorageBlockGB)
		}
		blocks := int64(sizeGB) / catalog.StorageBlockGB
		chargeUSDMicros = blocks * catalog.StorageBlockPriceUSDMicros
		snapshot["sizeGb"], snapshot["chargeUsdMicros"] = sizeGB, chargeUSDMicros
	}
	return map[string]any{
		"priceVersion": catalog.Version, "pricingVersion": catalog.Version, "resourceType": resourceType, "packageId": plan.ID,
		"currency": catalog.Currency, "displayCurrency": catalog.Currency, "billingUnit": catalog.BillingUnit,
		"chargeUsdMicros": chargeUSDMicros,
		"priceSnapshot":   snapshot,
	}, nil
}

func catalogStorageLimitGB(catalog pricingCatalogData) (int64, bool) {
	if catalog.StorageBlockGB <= 0 || catalog.StorageBlockGB > int64(maxJSONSafeInteger) || catalog.StorageBlockPriceUSDMicros <= 0 {
		return 0, false
	}
	maxBlocks := int64(maxJSONSafeInteger) / catalog.StorageBlockGB
	if chargeBlocks := int64(math.MaxInt64) / catalog.StorageBlockPriceUSDMicros; chargeBlocks < maxBlocks {
		maxBlocks = chargeBlocks
	}
	if maxBlocks <= 0 {
		return 0, false
	}
	return maxBlocks * catalog.StorageBlockGB, true
}

func workspacePricingPreview(catalog pricingCatalogData, input map[string]any) (map[string]any, error) {
	packageID, validPackage := input["packageId"].(string)
	packageID = strings.TrimSpace(packageID)
	sizeGB, validSize := positiveIntegerField(input, "sizeGb")
	if !validPackage || packageID == "" || !validSize {
		return nil, fmt.Errorf("%w: invalid workspace package or storage size", errInvalidPricingInput)
	}
	computeInput := cloneMap(input)
	computeInput["resourceType"], computeInput["packageId"] = "compute", packageID
	storageInput := cloneMap(input)
	storageInput["resourceType"], storageInput["packageId"], storageInput["sizeGb"] = "storage", packageID, sizeGB
	compute, err := pricingPreviewFromCatalog(catalog, computeInput)
	if err != nil {
		return nil, err
	}
	storage, err := pricingPreviewFromCatalog(catalog, storageInput)
	if err != nil {
		return nil, err
	}
	usdMicros, ok := checkedAddInt64(int64(numberField(compute, "chargeUsdMicros", 0)), int64(numberField(storage, "chargeUsdMicros", 0)))
	if !ok {
		return nil, errInvalidPricingInput
	}
	return map[string]any{
		"resourceType": "workspace", "priceVersion": catalog.Version, "pricingVersion": catalog.Version,
		"packageId": stringValue(compute["packageId"]), "currency": catalog.Currency, "displayCurrency": catalog.Currency,
		"billingUnit": catalog.BillingUnit, "compute": compute, "storage": storage,
		"totalChargeUsdMicros": usdMicros,
	}, nil
}

func checkedAddInt64(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

// packageRows projects the catalog's packages into the Console's package
// identity rows. The rows carry no price: the price of a new resource is the
// Resource Catalog's to publish.
func packageRows(catalog pricingCatalogData) []any {
	rows := make([]any, 0, len(catalog.Packages))
	for _, plan := range catalog.Packages {
		rows = append(rows, map[string]any{
			"id": plan.ID, "name": plan.Name, "available": plan.Available,
		})
	}
	return rows
}

func packageRowsForComputePools(catalog pricingCatalogData, computePools []any) []any {
	available := map[string]bool{}
	names := map[string]string{}
	for _, raw := range computePools {
		pool, _ := raw.(map[string]any)
		packageID := stringValue(pool["packageId"])
		if packageID == "" {
			continue
		}
		available[packageID] = available[packageID] || pool["available"] == true
		if name := strings.TrimSpace(stringValue(pool["name"])); name != "" {
			names[packageID] = name
		}
	}
	rows := packageRows(catalog)
	for _, raw := range rows {
		row := raw.(map[string]any)
		if name := names[stringValue(row["id"])]; name != "" {
			row["name"] = name
		}
		row["available"] = row["available"] == true && available[stringValue(row["id"])]
	}
	return rows
}

func packageByIDFromCatalog(catalog pricingCatalogData, packageID string) (pricingPackageData, bool) {
	for _, plan := range catalog.Packages {
		if plan.ID == packageID {
			return plan, true
		}
	}
	return pricingPackageData{}, false
}

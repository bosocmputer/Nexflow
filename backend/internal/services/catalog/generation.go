package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"nexflow/internal/repository"
	"nexflow/internal/services/sml"
)

const catalogGenerationLeaseDuration = 2 * time.Minute

type stockCatalogPager interface {
	CatalogRange(ctx context.Context, page, size int, updatedFrom, updatedTo *time.Time) (*sml.StockCatalogPage, error)
}

type catalogGenerationStore interface {
	AcquireCatalogGenerationLease(context.Context, string, time.Duration) (int64, error)
	RenewCatalogGenerationLease(context.Context, string, int64, time.Duration) error
	ReleaseCatalogGenerationLease(context.Context, string, int64) error
	BeginCatalogGeneration(context.Context, string, int64, time.Time) (string, error)
	StageCatalogGenerationPage(context.Context, string, string, int64, repository.CatalogGenerationPage) error
	ActivateCatalogGeneration(context.Context, string, string, int64, time.Time) error
	FailCatalogGeneration(context.Context, string, string) error
}

type normalizedUnitCatalogPage struct {
	Products      []repository.CatalogGenerationProduct
	Units         []repository.CatalogGenerationUnit
	Barcodes      []repository.CatalogGenerationBarcode
	SetComponents []repository.CatalogGenerationSetComponent
}

func normalizeUnitCatalogPage(items []sml.StockCatalogItem) (normalizedUnitCatalogPage, error) {
	page := normalizedUnitCatalogPage{
		Products: make([]repository.CatalogGenerationProduct, 0, len(items)),
	}
	for _, item := range items {
		itemCode := strings.TrimSpace(item.ItemCode)
		standardUnit := strings.TrimSpace(item.StandardUnit)
		if itemCode == "" {
			return normalizedUnitCatalogPage{}, fmt.Errorf("SML stock catalog contains an empty item code")
		}
		if standardUnit == "" {
			return normalizedUnitCatalogPage{}, fmt.Errorf("SML item %s has no standard unit", itemCode)
		}

		page.Products = append(page.Products, repository.CatalogGenerationProduct{
			ItemCode: itemCode, ItemName: strings.TrimSpace(item.ItemName),
			StandardUnit: standardUnit, ItemType: item.ItemType, SourceUpdatedAt: item.UpdatedAt,
		})
		defaultFound := false
		seenUnits := make(map[string]struct{}, len(item.Units))
		for _, unit := range item.Units {
			unitCode := strings.TrimSpace(unit.Code)
			if unitCode == "" {
				return normalizedUnitCatalogPage{}, fmt.Errorf("SML item %s contains an empty unit code", itemCode)
			}
			if _, exists := seenUnits[unitCode]; exists {
				return normalizedUnitCatalogPage{}, fmt.Errorf("SML item %s contains duplicate unit %s", itemCode, unitCode)
			}
			seenUnits[unitCode] = struct{}{}
			standValue, standOK := exactCatalogFactor(unit.StandValueExact, unit.StandValue)
			divideValue, divideOK := exactCatalogFactor(unit.DivideValueExact, unit.DivideValue)
			if !standOK || !divideOK {
				return normalizedUnitCatalogPage{}, fmt.Errorf("SML item %s unit %s has invalid stand/divide", itemCode, unitCode)
			}
			isDefault := unitCode == standardUnit
			defaultFound = defaultFound || isDefault
			page.Units = append(page.Units, repository.CatalogGenerationUnit{
				ItemCode: itemCode, UnitCode: unitCode, UnitName: strings.TrimSpace(unit.Name),
				StandValue: standValue, DivideValue: divideValue,
				IsDefault: isDefault, UnitOrder: unit.RowOrder,
			})
		}
		if !defaultFound {
			return normalizedUnitCatalogPage{}, fmt.Errorf("SML item %s standard unit %s is missing from ic_unit_use", itemCode, standardUnit)
		}

		for _, barcode := range item.Barcodes {
			code := strings.TrimSpace(barcode.Barcode)
			if code == "" {
				continue
			}
			page.Barcodes = append(page.Barcodes, repository.CatalogGenerationBarcode{
				ItemCode: itemCode, UnitCode: strings.TrimSpace(barcode.UnitCode), Barcode: code,
			})
		}
		if item.SetDefinition != nil {
			for _, component := range item.SetDefinition.Components {
				page.SetComponents = append(page.SetComponents, repository.CatalogGenerationSetComponent{
					ParentItemCode: itemCode, LineNumber: component.LineNumber, RowOrder: component.RowOrder,
					ItemCode: strings.TrimSpace(component.ItemCode), ItemName: strings.TrimSpace(component.ItemName),
					UnitCode: strings.TrimSpace(component.UnitCode), Qty: exactFactor(component.Qty),
					UnitFactor: exactFactor(component.UnitFactor), DefinitionHash: item.SetDefinition.Hash,
				})
			}
		}
	}
	return page, nil
}

func validPositiveFactor(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func exactFactor(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func exactCatalogFactor(exact string, fallback float64) (string, bool) {
	exact = strings.TrimSpace(exact)
	if exact == "" {
		if !validPositiveFactor(fallback) {
			return "", false
		}
		return exactFactor(fallback), true
	}
	value, ok := new(big.Rat).SetString(exact)
	if !ok || value.Sign() <= 0 {
		return "", false
	}
	return exact, true
}

func runUnitCatalogGeneration(ctx context.Context, store catalogGenerationStore, client stockCatalogPager, owner string, startedAt time.Time) (int, error) {
	fence, err := store.AcquireCatalogGenerationLease(ctx, owner, catalogGenerationLeaseDuration)
	if err != nil {
		return 0, err
	}
	defer store.ReleaseCatalogGenerationLease(context.Background(), owner, fence)

	generationID, err := store.BeginCatalogGeneration(ctx, owner, fence, startedAt)
	if err != nil {
		return 0, err
	}
	fail := func(cause error) (int, error) {
		_ = store.FailCatalogGeneration(context.Background(), generationID, cause.Error())
		return 0, cause
	}

	productHash := sha256.New()
	unitHash := sha256.New()
	var productCount, unitCount, barcodeCount, setCount int64
	complete := false
	for pageNumber := 1; pageNumber <= 10000; pageNumber++ {
		result, err := client.CatalogRange(ctx, pageNumber, 500, nil, &startedAt)
		if err != nil {
			return fail(err)
		}
		if result == nil {
			return fail(fmt.Errorf("SML stock catalog page %d returned no response", pageNumber))
		}
		if result.Page > 0 && result.Page != pageNumber {
			return fail(fmt.Errorf("SML stock catalog cursor changed: requested page %d, received %d", pageNumber, result.Page))
		}
		if len(result.Items) == 0 {
			if result.Total > int(productCount) {
				return fail(fmt.Errorf("SML stock catalog ended at %d of %d products", productCount, result.Total))
			}
			complete = true
			break
		}

		normalized, err := normalizeUnitCatalogPage(result.Items)
		if err != nil {
			return fail(err)
		}
		appendGenerationHashes(productHash, unitHash, normalized)
		productCount += int64(len(normalized.Products))
		unitCount += int64(len(normalized.Units))
		barcodeCount += int64(len(normalized.Barcodes))
		setCount += int64(len(normalized.SetComponents))

		if err := store.RenewCatalogGenerationLease(ctx, owner, fence, catalogGenerationLeaseDuration); err != nil {
			return fail(err)
		}
		if err := store.StageCatalogGenerationPage(ctx, generationID, owner, fence, repository.CatalogGenerationPage{
			Products: normalized.Products, Units: normalized.Units, Barcodes: normalized.Barcodes,
			SetComponents: normalized.SetComponents, Cursor: strconv.Itoa(pageNumber),
			ProductCount: productCount, UnitCount: unitCount, BarcodeCount: barcodeCount, SetCount: setCount,
			ProductHash: hashHex(productHash), UnitHash: hashHex(unitHash),
		}); err != nil {
			return fail(err)
		}
		if result.Total > 0 && int(productCount) >= result.Total {
			complete = true
			break
		}
	}
	if !complete {
		return fail(fmt.Errorf("SML stock catalog exceeded the 10,000 page safety limit"))
	}
	if err := store.ActivateCatalogGeneration(ctx, generationID, owner, fence, startedAt); err != nil {
		return fail(err)
	}
	return int(productCount), nil
}

func appendGenerationHashes(productHash, unitHash hash.Hash, page normalizedUnitCatalogPage) {
	for _, product := range page.Products {
		_, _ = fmt.Fprintf(productHash, "%s\x00%s\x00%s\x00%d\n",
			product.ItemCode, product.ItemName, product.StandardUnit, product.ItemType)
	}
	for _, unit := range page.Units {
		_, _ = fmt.Fprintf(unitHash, "%s\x00%s\x00%s\x00%s\x00%t\n",
			unit.ItemCode, unit.UnitCode, unit.StandValue, unit.DivideValue, unit.IsDefault)
	}
}

func hashHex(value hash.Hash) string {
	return hex.EncodeToString(value.Sum(nil))
}

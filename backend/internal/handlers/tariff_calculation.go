package handlers

import (
	"context"
	"database/sql"

	"cybercalc/internal/calculators"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
)

func calculateEntryAmount(ctx context.Context, db *sql.DB, code string, audience models.Audience, payload map[string]interface{}, reportYear int) (money.Amount, *string, error) {
	if code == "top_it" || code == "minc_decision" {
		amount, err := calculators.CalculateAmountWithRates(code, audience, payload, nil)
		return amount, nil, err
	}
	version, err := (tariffs.Repository{DB: db}).Active(ctx, reportYear, code, string(audience))
	if err != nil {
		return 0, nil, err
	}
	if !version.ProvenanceVerified {
		return 0, nil, tariffs.ErrUnverifiedProvenance
	}
	amount, err := calculators.CalculateAmountWithRates(code, audience, payload, version.Rates)
	if err != nil {
		return 0, nil, err
	}
	return amount, &version.ID, nil
}

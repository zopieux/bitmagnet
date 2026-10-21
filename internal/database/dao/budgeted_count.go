package dao

import (
	"gorm.io/gorm"
)

func ToSQL(db *gorm.DB) string {
	return db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Find(&[]interface{}{})
	})
}

type BudgetedCountResult struct {
	Count          int64
	Cost           float64
	BudgetExceeded bool
}

func BudgetedCount(db *gorm.DB, budget float64) (BudgetedCountResult, error) {
	// The Postgres budgeted_count() function does not exist in SQLite;
	// always do an exact count (the budget was only a Postgres optimization).
	q := ToSQL(db)
	row := db.
		Raw("SELECT count(*) as count, 0 as cost, false as budget_exceeded from (" + q + ") t").
		Row()

	result := BudgetedCountResult{}
	err := row.Scan(&result.Count, &result.Cost, &result.BudgetExceeded)

	return result, err
}

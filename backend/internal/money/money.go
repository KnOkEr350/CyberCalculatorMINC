// Package money stores rubles as integer kopecks, with decimal half-up rounding.
package money

import (
	"database/sql/driver"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
)

type Amount int64

func Add(a, b Amount) (Amount, error) {
	if a < 0 || b < 0 || int64(b) > math.MaxInt64-int64(a) {
		return 0, fmt.Errorf("суммарные затраты превышают допустимый предел")
	}
	return a + b, nil
}

var decimal = regexp.MustCompile(`^[0-9]{1,17}(\.[0-9]{1,12})?$`)

func Parse(s string) (Amount, error) {
	if !decimal.MatchString(s) {
		return 0, fmt.Errorf("сумма должна быть десятичным числом без экспоненты")
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("некорректная сумма")
	}
	return FromRat(r)
}

func FromRat(r *big.Rat) (Amount, error) {
	if r.Sign() < 0 {
		return 0, fmt.Errorf("отрицательная сумма")
	}
	scaled := new(big.Rat).Mul(r, big.NewRat(100, 1))
	whole, rest := new(big.Int), new(big.Int)
	whole.QuoRem(scaled.Num(), scaled.Denom(), rest)
	if new(big.Int).Mul(rest, big.NewInt(2)).Cmp(scaled.Denom()) >= 0 {
		whole.Add(whole, big.NewInt(1))
	}
	if !whole.IsInt64() {
		return 0, fmt.Errorf("слишком большая сумма")
	}
	return Amount(whole.Int64()), nil
}

func (a Amount) String() string               { return fmt.Sprintf("%d.%02d", int64(a)/100, int64(a)%100) }
func (a Amount) MarshalJSON() ([]byte, error) { return []byte(a.String()), nil }
func (a *Amount) UnmarshalJSON(b []byte) error {
	v, err := Parse(string(b))
	if err == nil {
		*a = v
	}
	return err
}
func (a Amount) Value() (driver.Value, error) { return a.String(), nil }
func (a *Amount) Scan(src interface{}) error {
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	case int64:
		s = strconv.FormatInt(v, 10)
	default:
		return fmt.Errorf("неподдерживаемый тип суммы %T", src)
	}
	v, err := Parse(s)
	if err == nil {
		*a = v
	}
	return err
}
func (a Amount) Rubles() float64 { return float64(a) / 100 }

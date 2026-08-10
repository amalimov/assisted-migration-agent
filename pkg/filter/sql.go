package filter

import (
	"errors"
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
)

// MapFunc resolves a filter identifier (e.g. "memory") to a fully qualified
// SQL column reference (e.g. m."Size MB") and its expected FieldType.
// The function should return an error for unknown identifiers.
type MapFunc func(name string) (string, FieldType, error)

func toSql(expr Expression, mf MapFunc) (sq.Sqlizer, error) {
	switch e := expr.(type) {
	case *binaryExpression:
		if e.Op != and && e.Op != or {
			if v, ok := e.Left.(*varExpression); ok {
				_, fieldType, err := mf(strings.ToLower(v.Name))
				if err != nil {
					return nil, err
				}
				if err := checkValueType(fieldType, e.Right); err != nil {
					return nil, fmt.Errorf("field %q is %s, but got %s value", v.Name, fieldType, e.Right.Type())
				}
			}
		}

		left, err := toSql(e.Left, mf)
		if err != nil {
			return nil, err
		}

		right, err := toSql(e.Right, mf)
		if err != nil {
			return nil, err
		}

		leftSQL, leftArgs, err := left.ToSql()
		if err != nil {
			return nil, err
		}

		rightSQL, rightArgs, err := right.ToSql()
		if err != nil {
			return nil, err
		}

		args := append(leftArgs, rightArgs...)
		switch e.Op {
		case like:
			return sq.Expr(fmt.Sprintf("regexp_matches(%s, %s)", leftSQL, rightSQL), args...), nil
		case notLike:
			return sq.Expr(fmt.Sprintf("NOT regexp_matches(%s, %s)", leftSQL, rightSQL), args...), nil
		case and:
			return sq.And{left, right}, nil
		case or:
			return sq.Or{left, right}, nil
		case like2:
			pattern := fmt.Sprintf("%%%v%%", rightArgs[0])
			return sq.Expr(fmt.Sprintf("(%s %s ?)", leftSQL, e.Op.Sql()), append(leftArgs, pattern)...), nil
		default:
			return sq.Expr(fmt.Sprintf("(%s %s %s)", leftSQL, e.Op.Sql(), rightSQL), args...), nil
		}
	case *varExpression:
		col, _, err := mf(strings.ToLower(e.Name))
		if err != nil {
			return nil, err
		}
		return sq.Expr(col), nil
	case *stringExpression:
		return sq.Expr("?", e.Value), nil
	case *booleanExpression:
		if e.Value {
			return sq.Expr("TRUE"), nil
		}
		return sq.Expr("FALSE"), nil
	case *regexExpression:
		return sq.Expr("?", e.Pattern), nil
	case *quantityExpression:
		var valueInMb float64
		switch e.Unit {
		case KbQuantityUnit:
			valueInMb = e.Value / 1024
		case MbQuantityUnit:
			valueInMb = e.Value
		case GbQuantityUnit:
			valueInMb = e.Value * 1024
		case TbQuantityUnit:
			valueInMb = e.Value * 1024 * 1024
		default:
			valueInMb = e.Value
		}
		return sq.Expr("?", valueInMb), nil
	case *inExpression:
		col, ft, err := mf(strings.ToLower(e.Left.(*varExpression).Name))
		if err != nil {
			return nil, err
		}
		if ft != StringField && ft != AnyField {
			return nil, fmt.Errorf("field %q is %s, but in/not in requires a string field", e.Left.(*varExpression).Name, ft)
		}
		if e.Negated {
			return sq.NotEq{col: e.Values}, nil
		}
		return sq.Eq{col: e.Values}, nil
	case *containsExpression:
		col, ft, err := mf(strings.ToLower(e.Left.(*varExpression).Name))
		if err != nil {
			return nil, err
		}
		if ft != ArrayField && ft != AnyField {
			return nil, fmt.Errorf("field %q is %s, but contains/!contains requires an array field", e.Left.(*varExpression).Name, ft)
		}
		// Cast VARCHAR to VARCHAR[] for DuckDB's list_contains function
		castedCol := fmt.Sprintf("CAST(%s AS VARCHAR[])", col)
		if e.Negated {
			// Include NULLs in "not contains" results (VMs without any groups)
			return sq.Expr(fmt.Sprintf("(%s IS NULL OR NOT list_contains(%s, ?))", col, castedCol), e.Value), nil
		}
		return sq.Expr(fmt.Sprintf("list_contains(%s, ?)", castedCol), e.Value), nil
	default:
		return nil, fmt.Errorf("unknown expression type: %T", expr)
	}
}

// FieldType describes the expected value type for a filter field.
type FieldType int

const (
	// AnyField skips type validation. Use when field types are unknown.
	AnyField FieldType = iota
	StringField
	NumericField
	BooleanField
	ArrayField
)

func (f FieldType) String() string {
	switch f {
	case AnyField:
		return "any"
	case StringField:
		return "string"
	case NumericField:
		return "numeric"
	case BooleanField:
		return "boolean"
	case ArrayField:
		return "array"
	default:
		return "unknown"
	}
}

func checkValueType(ft FieldType, value Expression) error {
	switch ft {
	case AnyField:
		return nil
	case StringField:
		switch value.(type) {
		case *stringExpression, *regexExpression:
			return nil
		}
	case NumericField:
		if _, ok := value.(*quantityExpression); ok {
			return nil
		}
	case BooleanField:
		if _, ok := value.(*booleanExpression); ok {
			return nil
		}
	}
	return errors.New("type mismatched")
}

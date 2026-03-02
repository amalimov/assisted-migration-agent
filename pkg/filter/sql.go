package filter

import (
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
)

// MapFunc resolves a filter identifier (e.g. "memory") to a fully qualified
// SQL column reference (e.g. m."Size MB"). The function should return an
// error for unknown identifiers.
type MapFunc func(name string) (string, error)

var defaultMapFn MapFunc = func(name string) (string, error) {
	switch strings.ToLower(name) {
	case "id":
		return `v."VM ID"`, nil
	case "name":
		return `v."VM"`, nil
	case "powerstate", "status":
		return `v."Powerstate"`, nil
	case "cluster":
		return `v."Cluster"`, nil
	case "datacenter":
		return `v."Datacenter"`, nil
	case "memory":
		return `v."Memory"`, nil
	case "disk", "disksize":
		return `COALESCE(d.total_disk, 0)`, nil
	case "issues":
		return `COALESCE(c.issue_count, 0)`, nil
	case "template":
		return `v."Template"`, nil
	default:
		return "", fmt.Errorf("unknown filter field: %s", name)
	}
}

func toSql(expr Expression, mf MapFunc) (sq.Sqlizer, error) {
	switch e := expr.(type) {
	case *binaryExpression:
		left, err := toSql(e.Left, mf)
		if err != nil {
			return nil, err
		}
		right, err := toSql(e.Right, mf)
		if err != nil {
			return nil, err
		}
		leftSQL, leftArgs, _ := left.ToSql()
		rightSQL, rightArgs, _ := right.ToSql()
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
		default:
			return sq.Expr(fmt.Sprintf("(%s %s %s)", leftSQL, e.Op.Sql(), rightSQL), args...), nil
		}
	case *varExpression:
		col, err := mf(strings.ToLower(e.Name))
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
		return sq.Expr(fmt.Sprintf("%.2f", valueInMb)), nil
	case *inExpression:
		col, err := mf(strings.ToLower(e.Left.(*varExpression).Name))
		if err != nil {
			return nil, err
		}
		if e.Negated {
			return sq.NotEq{col: e.Values}, nil
		}
		return sq.Eq{col: e.Values}, nil
	default:
		return nil, fmt.Errorf("unknown expression type: %T", expr)
	}
}

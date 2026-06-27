package mcp_utility

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	defaultCalcPrecisionBits = 256
	minCalcPrecisionBits     = 32
	maxCalcPrecisionBits     = 1024
	maxExpressionRunes       = 20000
	maxExpressionTokens      = 12000
	maxExpressionDepth       = 256
	maxFactorialInput        = 1000
	maxResultTextRunes       = 6000
)

type calculateInput struct {
	Expression    string         `json:"expression"`
	Variables     map[string]any `json:"variables"`
	PrecisionBits int            `json:"precision_bits"`
	AngleUnit     string         `json:"angle_unit"`
}

func RunCalculate(ctx context.Context, args any) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	var input calculateInput
	if err := mapToolArgs(args, &input); err != nil {
		return nil, err
	}
	expression := strings.TrimSpace(input.Expression)
	if expression == "" {
		return nil, fmt.Errorf("expression is required")
	}
	if len([]rune(expression)) > maxExpressionRunes {
		return nil, fmt.Errorf("expression is too long")
	}
	precision := normalizedCalcPrecision(input.PrecisionBits)
	angleUnit := strings.ToLower(strings.TrimSpace(input.AngleUnit))
	if angleUnit == "" {
		angleUnit = "radian"
	}
	if angleUnit != "radian" && angleUnit != "degree" {
		return nil, fmt.Errorf("angle_unit must be radian or degree")
	}
	tokens, err := lexCalculateExpression(expression)
	if err != nil {
		return nil, err
	}
	if len(tokens) > maxExpressionTokens {
		return nil, fmt.Errorf("expression has too many tokens")
	}
	engine, err := newCalcEngine(precision, angleUnit, input.Variables)
	if err != nil {
		return nil, err
	}
	parser := calcParser{
		tokens: tokens,
		engine: engine,
	}
	value, err := parser.parse()
	if err != nil {
		return nil, err
	}
	output := map[string]any{
		"ok":             true,
		"expression":     expression,
		"precision_bits": precision,
		"angle_unit":     angleUnit,
		"result_count":   1,
	}
	if value.kind == calcBool {
		output["type"] = "boolean"
		output["value"] = strconv.FormatBool(value.boolean)
		output["boolean"] = value.boolean
		return output, nil
	}
	number := value.num
	text := formatCalcFloat(number, precision)
	output["type"] = "number"
	output["value"] = text
	if approx, _ := number.Float64(); !math.IsInf(approx, 0) && !math.IsNaN(approx) {
		output["approx"] = approx
	}
	if integer, ok := exactCalcInteger(number); ok {
		output["integer"] = integer.String()
	}
	if len([]rune(text)) >= maxResultTextRunes {
		output["truncated"] = true
		output["value"] = truncateRunes(text, maxResultTextRunes)
	}
	return output, nil
}

func normalizedCalcPrecision(value int) uint {
	if value <= 0 {
		value = defaultCalcPrecisionBits
	}
	if value < minCalcPrecisionBits {
		value = minCalcPrecisionBits
	}
	if value > maxCalcPrecisionBits {
		value = maxCalcPrecisionBits
	}
	return uint(value)
}

type calcTokenKind int

const (
	calcTokenEOF calcTokenKind = iota
	calcTokenNumber
	calcTokenIdent
	calcTokenOperator
	calcTokenLParen
	calcTokenRParen
	calcTokenComma
)

type calcToken struct {
	kind calcTokenKind
	text string
	pos  int
}

func lexCalculateExpression(expression string) ([]calcToken, error) {
	runes := []rune(expression)
	tokens := make([]calcToken, 0, len(runes)/2)
	for i := 0; i < len(runes); {
		r := runes[i]
		if unicode.IsSpace(r) {
			i++
			continue
		}
		if unicode.IsDigit(r) || r == '.' {
			start := i
			seenDigit := unicode.IsDigit(r)
			seenDot := r == '.'
			i++
			for i < len(runes) {
				next := runes[i]
				if unicode.IsDigit(next) {
					seenDigit = true
					i++
					continue
				}
				if next == '.' && !seenDot {
					seenDot = true
					i++
					continue
				}
				break
			}
			if i < len(runes) && (runes[i] == 'e' || runes[i] == 'E') {
				expIndex := i
				i++
				if i < len(runes) && (runes[i] == '+' || runes[i] == '-') {
					i++
				}
				expStart := i
				for i < len(runes) && unicode.IsDigit(runes[i]) {
					i++
				}
				if expStart == i {
					return nil, fmt.Errorf("invalid exponent at position %d", expIndex+1)
				}
			}
			if !seenDigit {
				return nil, fmt.Errorf("invalid number at position %d", start+1)
			}
			tokens = append(tokens, calcToken{kind: calcTokenNumber, text: string(runes[start:i]), pos: start})
			continue
		}
		if unicode.IsLetter(r) || r == '_' {
			start := i
			i++
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			tokens = append(tokens, calcToken{kind: calcTokenIdent, text: string(runes[start:i]), pos: start})
			continue
		}
		if i+1 < len(runes) {
			two := string(runes[i : i+2])
			switch two {
			case ">=", "<=", "==", "!=", "&&", "||":
				tokens = append(tokens, calcToken{kind: calcTokenOperator, text: two, pos: i})
				i += 2
				continue
			}
		}
		switch r {
		case '+', '-', '*', '/', '%', '^', '<', '>', '!':
			tokens = append(tokens, calcToken{kind: calcTokenOperator, text: string(r), pos: i})
		case '(':
			tokens = append(tokens, calcToken{kind: calcTokenLParen, text: string(r), pos: i})
		case ')':
			tokens = append(tokens, calcToken{kind: calcTokenRParen, text: string(r), pos: i})
		case ',':
			tokens = append(tokens, calcToken{kind: calcTokenComma, text: string(r), pos: i})
		default:
			return nil, fmt.Errorf("unexpected character %q at position %d", r, i+1)
		}
		i++
	}
	tokens = append(tokens, calcToken{kind: calcTokenEOF, pos: len(runes)})
	return tokens, nil
}

type calcValueKind int

const (
	calcNumber calcValueKind = iota
	calcBool
)

type calcValue struct {
	kind    calcValueKind
	num     *big.Float
	boolean bool
}

func calcNumberValue(value *big.Float) calcValue {
	return calcValue{kind: calcNumber, num: value}
}

func calcBoolValue(value bool) calcValue {
	return calcValue{kind: calcBool, boolean: value}
}

type calcEngine struct {
	precision uint
	angleUnit string
	variables map[string]calcValue
}

func newCalcEngine(precision uint, angleUnit string, variables map[string]any) (*calcEngine, error) {
	engine := &calcEngine{
		precision: precision,
		angleUnit: angleUnit,
		variables: map[string]calcValue{},
	}
	for name, value := range variables {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			continue
		}
		parsed, err := engine.valueFromAny(value)
		if err != nil {
			return nil, fmt.Errorf("invalid variable %q: %w", name, err)
		}
		engine.variables[normalized] = parsed
	}
	return engine, nil
}

func (e *calcEngine) newFloat() *big.Float {
	return new(big.Float).SetPrec(e.precision).SetMode(big.ToNearestEven)
}

func (e *calcEngine) floatFromString(value string) (*big.Float, error) {
	parsed := e.newFloat()
	if _, ok := parsed.SetString(value); !ok {
		return nil, fmt.Errorf("invalid number %q", value)
	}
	return parsed, nil
}

func (e *calcEngine) floatFromFloat64(value float64) (*big.Float, error) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return nil, fmt.Errorf("calculation produced a non-finite number")
	}
	return e.newFloat().SetFloat64(value), nil
}

func (e *calcEngine) valueFromAny(value any) (calcValue, error) {
	switch typed := value.(type) {
	case nil:
		return calcValue{}, fmt.Errorf("nil is not numeric")
	case bool:
		return calcBoolValue(typed), nil
	case int:
		return calcNumberValue(e.newFloat().SetInt64(int64(typed))), nil
	case int64:
		return calcNumberValue(e.newFloat().SetInt64(typed)), nil
	case float64:
		number, err := e.floatFromFloat64(typed)
		if err != nil {
			return calcValue{}, err
		}
		return calcNumberValue(number), nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return calcValue{}, fmt.Errorf("empty string is not numeric")
		}
		switch strings.ToLower(text) {
		case "true":
			return calcBoolValue(true), nil
		case "false":
			return calcBoolValue(false), nil
		}
		number, err := e.floatFromString(text)
		if err != nil {
			return calcValue{}, err
		}
		return calcNumberValue(number), nil
	default:
		return calcValue{}, fmt.Errorf("unsupported type %T", value)
	}
}

func (e *calcEngine) number(value calcValue) (*big.Float, error) {
	if value.kind == calcNumber {
		return e.newFloat().Set(value.num), nil
	}
	if value.boolean {
		return e.newFloat().SetInt64(1), nil
	}
	return e.newFloat().SetInt64(0), nil
}

func (e *calcEngine) truthy(value calcValue) (bool, error) {
	if value.kind == calcBool {
		return value.boolean, nil
	}
	return value.num.Sign() != 0, nil
}

func (e *calcEngine) constant(name string) (calcValue, bool, error) {
	switch strings.ToLower(name) {
	case "pi":
		number, err := e.floatFromFloat64(math.Pi)
		return calcNumberValue(number), true, err
	case "tau":
		number, err := e.floatFromFloat64(2 * math.Pi)
		return calcNumberValue(number), true, err
	case "e":
		number, err := e.floatFromFloat64(math.E)
		return calcNumberValue(number), true, err
	case "phi":
		number, err := e.floatFromFloat64((1 + math.Sqrt(5)) / 2)
		return calcNumberValue(number), true, err
	case "true":
		return calcBoolValue(true), true, nil
	case "false":
		return calcBoolValue(false), true, nil
	default:
		value, ok := e.variables[strings.ToLower(name)]
		return value, ok, nil
	}
}

type calcParser struct {
	tokens []calcToken
	pos    int
	engine *calcEngine
	depth  int
}

func (p *calcParser) parse() (calcValue, error) {
	value, err := p.parseExpression(1)
	if err != nil {
		return calcValue{}, err
	}
	if token := p.peek(); token.kind != calcTokenEOF {
		return calcValue{}, fmt.Errorf("unexpected token %q at position %d", token.text, token.pos+1)
	}
	return value, nil
}

func (p *calcParser) parseExpression(minPrecedence int) (calcValue, error) {
	if p.depth > maxExpressionDepth {
		return calcValue{}, fmt.Errorf("expression nesting is too deep")
	}
	p.depth++
	defer func() {
		p.depth--
	}()
	left, err := p.parsePrefix()
	if err != nil {
		return calcValue{}, err
	}
	for {
		token := p.peek()
		if token.kind == calcTokenOperator && token.text == "!" {
			p.next()
			left, err = p.applyFactorial(left)
			if err != nil {
				return calcValue{}, err
			}
			continue
		}
		if token.kind == calcTokenOperator && token.text == "%" && p.percentIsPostfix() {
			p.next()
			left, err = p.divideByHundred(left)
			if err != nil {
				return calcValue{}, err
			}
			continue
		}
		op, implicit := "", false
		precedence, rightAssociative := 0, false
		if startsPrimary(token) {
			op = "*"
			implicit = true
			precedence = calcOperatorPrecedence(op)
		} else if token.kind == calcTokenOperator {
			op = token.text
			precedence = calcOperatorPrecedence(op)
			rightAssociative = op == "^"
		}
		if precedence == 0 || precedence < minPrecedence {
			break
		}
		if !implicit {
			p.next()
		}
		nextMin := precedence + 1
		if rightAssociative {
			nextMin = precedence
		}
		right, err := p.parseExpression(nextMin)
		if err != nil {
			return calcValue{}, err
		}
		left, err = p.applyBinary(op, left, right)
		if err != nil {
			return calcValue{}, err
		}
	}
	return left, nil
}

func (p *calcParser) parsePrefix() (calcValue, error) {
	token := p.next()
	switch token.kind {
	case calcTokenNumber:
		number, err := p.engine.floatFromString(token.text)
		if err != nil {
			return calcValue{}, fmt.Errorf("%w at position %d", err, token.pos+1)
		}
		return calcNumberValue(number), nil
	case calcTokenIdent:
		if p.peek().kind == calcTokenLParen {
			return p.parseFunctionCall(token)
		}
		value, ok, err := p.engine.constant(token.text)
		if err != nil {
			return calcValue{}, err
		}
		if !ok {
			return calcValue{}, fmt.Errorf("unknown identifier %q at position %d", token.text, token.pos+1)
		}
		return value, nil
	case calcTokenOperator:
		switch token.text {
		case "+":
			return p.parseExpression(6)
		case "-":
			value, err := p.parseExpression(6)
			if err != nil {
				return calcValue{}, err
			}
			number, err := p.engine.number(value)
			if err != nil {
				return calcValue{}, err
			}
			return calcNumberValue(p.engine.newFloat().Neg(number)), nil
		case "!":
			value, err := p.parseExpression(6)
			if err != nil {
				return calcValue{}, err
			}
			truthy, err := p.engine.truthy(value)
			if err != nil {
				return calcValue{}, err
			}
			return calcBoolValue(!truthy), nil
		default:
			return calcValue{}, fmt.Errorf("unexpected operator %q at position %d", token.text, token.pos+1)
		}
	case calcTokenLParen:
		value, err := p.parseExpression(1)
		if err != nil {
			return calcValue{}, err
		}
		if p.peek().kind != calcTokenRParen {
			return calcValue{}, fmt.Errorf("expected ')' at position %d", p.peek().pos+1)
		}
		p.next()
		return value, nil
	default:
		return calcValue{}, fmt.Errorf("unexpected token %q at position %d", token.text, token.pos+1)
	}
}

func (p *calcParser) parseFunctionCall(nameToken calcToken) (calcValue, error) {
	p.next()
	args := []calcValue{}
	if p.peek().kind != calcTokenRParen {
		for {
			arg, err := p.parseExpression(1)
			if err != nil {
				return calcValue{}, err
			}
			args = append(args, arg)
			if p.peek().kind != calcTokenComma {
				break
			}
			p.next()
		}
	}
	if p.peek().kind != calcTokenRParen {
		return calcValue{}, fmt.Errorf("expected ')' after function %s", nameToken.text)
	}
	p.next()
	return p.applyFunction(nameToken.text, args)
}

func (p *calcParser) peek() calcToken {
	if p.pos >= len(p.tokens) {
		return calcToken{kind: calcTokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *calcParser) next() calcToken {
	token := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return token
}

func startsPrimary(token calcToken) bool {
	if token.kind == calcTokenNumber || token.kind == calcTokenIdent || token.kind == calcTokenLParen {
		return true
	}
	return false
}

func calcOperatorPrecedence(op string) int {
	switch op {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=", "<", "<=", ">", ">=":
		return 3
	case "+", "-":
		return 4
	case "*", "/", "%":
		return 5
	case "^":
		return 6
	default:
		return 0
	}
}

func (p *calcParser) percentIsPostfix() bool {
	if p.pos+1 >= len(p.tokens) {
		return true
	}
	next := p.tokens[p.pos+1]
	if next.kind == calcTokenEOF || next.kind == calcTokenRParen || next.kind == calcTokenComma {
		return true
	}
	if next.kind == calcTokenOperator {
		return true
	}
	return false
}

func (p *calcParser) applyBinary(op string, left calcValue, right calcValue) (calcValue, error) {
	switch op {
	case "+", "-", "*", "/", "%", "^":
		leftNumber, err := p.engine.number(left)
		if err != nil {
			return calcValue{}, err
		}
		rightNumber, err := p.engine.number(right)
		if err != nil {
			return calcValue{}, err
		}
		return p.applyNumericBinary(op, leftNumber, rightNumber)
	case "==", "!=", "<", "<=", ">", ">=":
		leftNumber, err := p.engine.number(left)
		if err != nil {
			return calcValue{}, err
		}
		rightNumber, err := p.engine.number(right)
		if err != nil {
			return calcValue{}, err
		}
		comparison := leftNumber.Cmp(rightNumber)
		switch op {
		case "==":
			return calcBoolValue(comparison == 0), nil
		case "!=":
			return calcBoolValue(comparison != 0), nil
		case "<":
			return calcBoolValue(comparison < 0), nil
		case "<=":
			return calcBoolValue(comparison <= 0), nil
		case ">":
			return calcBoolValue(comparison > 0), nil
		case ">=":
			return calcBoolValue(comparison >= 0), nil
		}
	case "&&", "||":
		leftTruth, err := p.engine.truthy(left)
		if err != nil {
			return calcValue{}, err
		}
		rightTruth, err := p.engine.truthy(right)
		if err != nil {
			return calcValue{}, err
		}
		if op == "&&" {
			return calcBoolValue(leftTruth && rightTruth), nil
		}
		return calcBoolValue(leftTruth || rightTruth), nil
	}
	return calcValue{}, fmt.Errorf("unsupported operator %q", op)
}

func (p *calcParser) applyNumericBinary(op string, left *big.Float, right *big.Float) (calcValue, error) {
	result := p.engine.newFloat()
	switch op {
	case "+":
		return calcNumberValue(result.Add(left, right)), nil
	case "-":
		return calcNumberValue(result.Sub(left, right)), nil
	case "*":
		return calcNumberValue(result.Mul(left, right)), nil
	case "/":
		if right.Sign() == 0 {
			return calcValue{}, fmt.Errorf("division by zero")
		}
		return calcNumberValue(result.Quo(left, right)), nil
	case "%":
		return p.applyModulo(left, right)
	case "^":
		return p.applyPower(left, right)
	default:
		return calcValue{}, fmt.Errorf("unsupported operator %q", op)
	}
}

func (p *calcParser) applyModulo(left *big.Float, right *big.Float) (calcValue, error) {
	if right.Sign() == 0 {
		return calcValue{}, fmt.Errorf("modulo by zero")
	}
	if leftInt, ok := exactCalcInteger(left); ok {
		if rightInt, ok := exactCalcInteger(right); ok {
			result := new(big.Int).Mod(leftInt, rightInt)
			return calcNumberValue(p.engine.newFloat().SetInt(result)), nil
		}
	}
	leftFloat, _ := left.Float64()
	rightFloat, _ := right.Float64()
	result, err := p.engine.floatFromFloat64(math.Mod(leftFloat, rightFloat))
	if err != nil {
		return calcValue{}, err
	}
	return calcNumberValue(result), nil
}

func (p *calcParser) applyPower(left *big.Float, right *big.Float) (calcValue, error) {
	if exponent, ok := exactCalcInteger(right); ok && exponent.BitLen() < 16 {
		exp := int(exponent.Int64())
		if exp >= -1024 && exp <= 1024 {
			return p.applyIntegerPower(left, exp)
		}
	}
	baseFloat, _ := left.Float64()
	expFloat, _ := right.Float64()
	result, err := p.engine.floatFromFloat64(math.Pow(baseFloat, expFloat))
	if err != nil {
		return calcValue{}, err
	}
	return calcNumberValue(result), nil
}

func (p *calcParser) applyIntegerPower(base *big.Float, exponent int) (calcValue, error) {
	if exponent == 0 {
		return calcNumberValue(p.engine.newFloat().SetInt64(1)), nil
	}
	negative := exponent < 0
	if negative {
		exponent = -exponent
	}
	result := p.engine.newFloat().SetInt64(1)
	factor := p.engine.newFloat().Set(base)
	for exponent > 0 {
		if exponent%2 == 1 {
			result.Mul(result, factor)
		}
		exponent /= 2
		if exponent > 0 {
			factor.Mul(factor, factor)
		}
	}
	if negative {
		if result.Sign() == 0 {
			return calcValue{}, fmt.Errorf("division by zero")
		}
		result.Quo(p.engine.newFloat().SetInt64(1), result)
	}
	return calcNumberValue(result), nil
}

func (p *calcParser) applyFactorial(value calcValue) (calcValue, error) {
	number, err := p.engine.number(value)
	if err != nil {
		return calcValue{}, err
	}
	integer, ok := exactCalcInteger(number)
	if !ok {
		return calcValue{}, fmt.Errorf("factorial requires an integer")
	}
	if integer.Sign() < 0 {
		return calcValue{}, fmt.Errorf("factorial requires a non-negative integer")
	}
	if integer.BitLen() > 31 || integer.Int64() > maxFactorialInput {
		return calcValue{}, fmt.Errorf("factorial input is too large")
	}
	result := big.NewInt(1)
	limit := int(integer.Int64())
	for i := 2; i <= limit; i++ {
		result.Mul(result, big.NewInt(int64(i)))
	}
	return calcNumberValue(p.engine.newFloat().SetInt(result)), nil
}

func (p *calcParser) divideByHundred(value calcValue) (calcValue, error) {
	number, err := p.engine.number(value)
	if err != nil {
		return calcValue{}, err
	}
	return calcNumberValue(p.engine.newFloat().Quo(number, p.engine.newFloat().SetInt64(100))), nil
}

func (p *calcParser) applyFunction(name string, args []calcValue) (calcValue, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "abs":
		if err := requireCalcArgCount(normalized, args, 1); err != nil {
			return calcValue{}, err
		}
		number, err := p.engine.number(args[0])
		if err != nil {
			return calcValue{}, err
		}
		return calcNumberValue(p.engine.newFloat().Abs(number)), nil
	case "sqrt", "cbrt", "exp", "ln", "log", "log10", "log2", "sin", "cos", "tan", "asin", "acos", "atan", "floor", "ceil", "round":
		return p.applyUnaryFloatFunction(normalized, args)
	case "pow":
		if err := requireCalcArgCount(normalized, args, 2); err != nil {
			return calcValue{}, err
		}
		left, err := p.engine.number(args[0])
		if err != nil {
			return calcValue{}, err
		}
		right, err := p.engine.number(args[1])
		if err != nil {
			return calcValue{}, err
		}
		return p.applyPower(left, right)
	case "min", "max", "sum", "avg", "mean", "product", "median":
		return p.applyAggregateFunction(normalized, args)
	case "gcd", "lcm":
		return p.applyIntegerAggregateFunction(normalized, args)
	case "if":
		if err := requireCalcArgCount(normalized, args, 3); err != nil {
			return calcValue{}, err
		}
		truthy, err := p.engine.truthy(args[0])
		if err != nil {
			return calcValue{}, err
		}
		if truthy {
			return args[1], nil
		}
		return args[2], nil
	default:
		return calcValue{}, fmt.Errorf("unknown function %q", name)
	}
}

func (p *calcParser) applyUnaryFloatFunction(name string, args []calcValue) (calcValue, error) {
	if err := requireCalcArgCount(name, args, 1); err != nil {
		return calcValue{}, err
	}
	number, err := p.engine.number(args[0])
	if err != nil {
		return calcValue{}, err
	}
	input, _ := number.Float64()
	switch name {
	case "sin", "cos", "tan":
		input = p.toRadians(input)
	}
	var result float64
	switch name {
	case "sqrt":
		result = math.Sqrt(input)
	case "cbrt":
		result = math.Cbrt(input)
	case "exp":
		result = math.Exp(input)
	case "ln", "log":
		result = math.Log(input)
	case "log10":
		result = math.Log10(input)
	case "log2":
		result = math.Log2(input)
	case "sin":
		result = math.Sin(input)
	case "cos":
		result = math.Cos(input)
	case "tan":
		result = math.Tan(input)
	case "asin":
		result = math.Asin(input)
		result = p.fromRadians(result)
	case "acos":
		result = math.Acos(input)
		result = p.fromRadians(result)
	case "atan":
		result = math.Atan(input)
		result = p.fromRadians(result)
	case "floor":
		result = math.Floor(input)
	case "ceil":
		result = math.Ceil(input)
	case "round":
		result = math.Round(input)
	default:
		return calcValue{}, fmt.Errorf("unknown function %q", name)
	}
	parsed, err := p.engine.floatFromFloat64(result)
	if err != nil {
		return calcValue{}, err
	}
	return calcNumberValue(parsed), nil
}

func (p *calcParser) applyAggregateFunction(name string, args []calcValue) (calcValue, error) {
	if len(args) == 0 {
		return calcValue{}, fmt.Errorf("%s requires at least one argument", name)
	}
	values := make([]*big.Float, 0, len(args))
	for _, arg := range args {
		number, err := p.engine.number(arg)
		if err != nil {
			return calcValue{}, err
		}
		values = append(values, number)
	}
	switch name {
	case "min":
		result := p.engine.newFloat().Set(values[0])
		for _, value := range values[1:] {
			if value.Cmp(result) < 0 {
				result.Set(value)
			}
		}
		return calcNumberValue(result), nil
	case "max":
		result := p.engine.newFloat().Set(values[0])
		for _, value := range values[1:] {
			if value.Cmp(result) > 0 {
				result.Set(value)
			}
		}
		return calcNumberValue(result), nil
	case "sum", "avg", "mean":
		result := p.engine.newFloat().SetInt64(0)
		for _, value := range values {
			result.Add(result, value)
		}
		if name == "avg" || name == "mean" {
			result.Quo(result, p.engine.newFloat().SetInt64(int64(len(values))))
		}
		return calcNumberValue(result), nil
	case "product":
		result := p.engine.newFloat().SetInt64(1)
		for _, value := range values {
			result.Mul(result, value)
		}
		return calcNumberValue(result), nil
	case "median":
		sort.Slice(values, func(i int, j int) bool {
			return values[i].Cmp(values[j]) < 0
		})
		mid := len(values) / 2
		if len(values)%2 == 1 {
			return calcNumberValue(p.engine.newFloat().Set(values[mid])), nil
		}
		result := p.engine.newFloat().Add(values[mid-1], values[mid])
		result.Quo(result, p.engine.newFloat().SetInt64(2))
		return calcNumberValue(result), nil
	default:
		return calcValue{}, fmt.Errorf("unknown aggregate function %q", name)
	}
}

func (p *calcParser) applyIntegerAggregateFunction(name string, args []calcValue) (calcValue, error) {
	if len(args) == 0 {
		return calcValue{}, fmt.Errorf("%s requires at least one argument", name)
	}
	var result *big.Int
	for index, arg := range args {
		number, err := p.engine.number(arg)
		if err != nil {
			return calcValue{}, err
		}
		integer, ok := exactCalcInteger(number)
		if !ok {
			return calcValue{}, fmt.Errorf("%s requires integer arguments", name)
		}
		integer.Abs(integer)
		if index == 0 {
			result = integer
			continue
		}
		switch name {
		case "gcd":
			result.GCD(nil, nil, result, integer)
		case "lcm":
			if result.Sign() == 0 || integer.Sign() == 0 {
				result.SetInt64(0)
				continue
			}
			gcd := new(big.Int).GCD(nil, nil, result, integer)
			result.Mul(result, integer)
			result.Abs(result)
			result.Div(result, gcd)
		}
	}
	return calcNumberValue(p.engine.newFloat().SetInt(result)), nil
}

func (p *calcParser) toRadians(value float64) float64 {
	if p.engine.angleUnit == "degree" {
		return value * math.Pi / 180
	}
	return value
}

func (p *calcParser) fromRadians(value float64) float64 {
	if p.engine.angleUnit == "degree" {
		return value * 180 / math.Pi
	}
	return value
}

func requireCalcArgCount(name string, args []calcValue, count int) error {
	if len(args) != count {
		return fmt.Errorf("%s requires %d argument(s)", name, count)
	}
	return nil
}

func exactCalcInteger(value *big.Float) (*big.Int, bool) {
	if value == nil {
		return nil, false
	}
	integer := new(big.Int)
	if _, accuracy := value.Int(integer); accuracy != big.Exact {
		return nil, false
	}
	return integer, true
}

func formatCalcFloat(value *big.Float, precision uint) string {
	if value == nil {
		return ""
	}
	if integer, ok := exactCalcInteger(value); ok {
		return integer.String()
	}
	digits := int(float64(precision)*0.30103) + 2
	if digits < 16 {
		digits = 16
	}
	if digits > 160 {
		digits = 160
	}
	return value.Text('g', digits)
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

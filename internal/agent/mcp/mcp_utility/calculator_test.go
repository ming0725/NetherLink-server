package mcp_utility

import (
	"context"
	"math"
	"testing"
)

func TestRunCalculateEvaluatesExpressionRules(t *testing.T) {
	output, err := RunCalculate(context.Background(), map[string]any{
		"expression": "sum(1,2,3,4)^2 + max(5, 9, 7) + gcd(84,126) + 50%",
	})
	if err != nil {
		t.Fatalf("RunCalculate() error = %v", err)
	}
	if output["ok"] != true || output["value"] != "151.5" {
		t.Fatalf("output = %#v, want value 151.5", output)
	}
}

func TestRunCalculateSupportsVariablesImplicitMultiplicationAndBooleans(t *testing.T) {
	output, err := RunCalculate(context.Background(), map[string]any{
		"expression": "2x + y == 10 && 5! == 120",
		"variables": map[string]any{
			"x": 3,
			"y": "4",
		},
	})
	if err != nil {
		t.Fatalf("RunCalculate() error = %v", err)
	}
	if output["type"] != "boolean" || output["boolean"] != true {
		t.Fatalf("output = %#v, want boolean true", output)
	}
}

func TestRunCalculateSupportsDegreeTrig(t *testing.T) {
	output, err := RunCalculate(context.Background(), map[string]any{
		"expression":     "sin(30) + cos(60)",
		"angle_unit":     "degree",
		"precision_bits": 128,
	})
	if err != nil {
		t.Fatalf("RunCalculate() error = %v", err)
	}
	approx, ok := output["approx"].(float64)
	if !ok || math.Abs(approx-1) > 1e-12 {
		t.Fatalf("approx = %#v, want near 1", output["approx"])
	}
}

func TestRunCalculatePowerPrecedenceBeatsUnaryMinus(t *testing.T) {
	output, err := RunCalculate(context.Background(), map[string]any{
		"expression": "-2^2",
	})
	if err != nil {
		t.Fatalf("RunCalculate() error = %v", err)
	}
	if output["value"] != "-4" {
		t.Fatalf("value = %#v, want -4", output["value"])
	}
}

func TestRunCalculateRejectsUnsafeOrInvalidExpressions(t *testing.T) {
	if _, err := RunCalculate(context.Background(), map[string]any{
		"expression": "unknown(1)",
	}); err == nil {
		t.Fatal("RunCalculate() error = nil, want unknown function error")
	}
	if _, err := RunCalculate(context.Background(), map[string]any{
		"expression": "1 / 0",
	}); err == nil {
		t.Fatal("RunCalculate() error = nil, want division by zero")
	}
}

//go:build bdd

// BDD feature tests for plugin-esphome.
// These run Cucumber/Gherkin scenarios against the live storage+messenger
// test environment (embedded NATS, no external infrastructure required).
//
// Run:
//
//	go test -tags bdd -v ./cmd/plugin-esphome/...
package main

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestBDDFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Name: "plugin-clean-bdd",
		ScenarioInitializer: func(ctx *godog.ScenarioContext) {
			c := newBDDCtx(t)
			c.RegisterSteps(ctx)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("BDD suite failed")
	}
}

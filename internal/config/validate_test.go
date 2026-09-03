package config

import (
	"errors"
	"strings"
	"testing"
)

func validateBody(t *testing.T, body string) error {
	t.Helper()
	cfg, err := Load(write(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg.Validate()
}

func TestValidateAcceptsGoodConfig(t *testing.T) {
	err := validateBody(t, `
version: 1
defaults: {environment: local, aliases: [queue]}
environments:
  local: {command: [klocal]}
  prod:  {context: Production, protected: true}
aliases:
  queue: queue
  api: {local: api-server, prod: api-gateway@backend}
`)
	if err != nil {
		t.Fatalf("want valid, got %v", err)
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	err := validateBody(t, `
version: 1
defaults: {environment: nowhere, aliases: [ghost]}
environments:
  bad_both:    {command: [klocal], context: Production}
  bad_neither: {}
aliases:
  api: {staging: backend}
  empty: {}
`)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	joined := ve.Error()
	for _, want := range []string{
		"bad_both",    // both command and context
		"bad_neither", // neither
		"nowhere",     // defaults.environment undefined
		"ghost",       // defaults.aliases names an unknown alias
		"staging",     // alias api maps an undefined environment
		"empty",       // alias defines no mapping at all
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems missing %q:\n%s", want, joined)
		}
	}
}

func TestValidateRejectsWrongVersion(t *testing.T) {
	err := validateBody(t, `
version: 2
environments: {local: {command: [klocal]}}
aliases: {queue: queue}
`)
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("want version complaint, got %v", err)
	}
}

func TestValidateRejectsEmptyWorkload(t *testing.T) {
	err := validateBody(t, `
version: 1
environments: {local: {command: [klocal]}}
aliases:
  broken: {local: {namespace: only-a-namespace}}
`)
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("want workload complaint, got %v", err)
	}
}

package config

import (
	"errors"
	"testing"
	"time"
)

func TestIssueTriggerPeriodDefaultsAndBounds(t *testing.T) {
	period, err := (IssueTriggerConfig{}).PollPeriod()
	if err != nil || period != DefaultIssuePeriod {
		t.Fatalf("default period = %v, %v; want %v", period, err, DefaultIssuePeriod)
	}
	if _, err := (IssueTriggerConfig{Period: "30s"}).PollPeriod(); !errors.Is(err, ErrIssueTriggerPeriod) {
		t.Fatalf("period 30s error = %v, want ErrIssueTriggerPeriod", err)
	}
	if period, err := (IssueTriggerConfig{Period: "2m"}).PollPeriod(); err != nil || period != 2*time.Minute {
		t.Fatalf("period 2m = %v, %v", period, err)
	}
}

func TestValidateIssueTriggers(t *testing.T) {
	cases := map[string]struct {
		triggers map[string]IssueTriggerConfig
		want     error
	}{
		"no repository form":   {map[string]IssueTriggerConfig{"herrscher": {Labels: []string{"ready"}}}, ErrIssueTriggerRepo},
		"no label or mention":  {map[string]IssueTriggerConfig{"github.com/a/b": {}}, ErrIssueTriggerEmpty},
		"blank label":          {map[string]IssueTriggerConfig{"github.com/a/b": {Labels: []string{"  "}}}, ErrIssueTriggerLabel},
		"bad mention":          {map[string]IssueTriggerConfig{"github.com/a/b": {Mentions: []string{"not a login"}}}, ErrIssueTriggerMention},
		"short period":         {map[string]IssueTriggerConfig{"github.com/a/b": {Labels: []string{"ready"}, Period: "5s"}}, ErrIssueTriggerPeriod},
		"valid":                {map[string]IssueTriggerConfig{"github.com/a/b": {Labels: []string{"ready"}, Mentions: []string{"alice"}}}, nil},
		"absent means nothing": {nil, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateIssueTriggers(tc.triggers)
			if tc.want == nil && err != nil {
				t.Fatalf("error = %v, want none", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

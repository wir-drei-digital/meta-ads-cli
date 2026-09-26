package config

import (
	"sort"
	"strings"
	"testing"
)

func TestOffsetTable(t *testing.T) {
	if len(offsets) != 66 {
		t.Fatalf("Meta's table lists 66 currencies, got %d", len(offsets))
	}
	var one []string
	for c, o := range offsets {
		if o == 1 {
			one = append(one, c)
		} else if o != 100 {
			t.Errorf("%s has offset %d", c, o)
		}
	}
	sort.Strings(one)
	if got := strings.Join(one, ","); got != "CLP,COP,CRC,HUF,IDR,ISK,JPY,KRW,PYG,TWD,VND" {
		t.Fatalf("offset-1 currencies: %s", got)
	}
	if o, ok := Offset("BHD"); !ok || o != 100 {
		t.Fatalf("BHD: %d %v (Meta uses 100, not ISO's 1000)", o, ok)
	}
}

func TestNormalizeCurrency(t *testing.T) {
	if c, err := NormalizeCurrency(" chf "); err != nil || c != "CHF" {
		t.Fatalf("got %q %v", c, err)
	}
	if _, err := NormalizeCurrency("XYZ"); err == nil {
		t.Fatal("an unknown currency was accepted")
	}
}

func TestParseMajor(t *testing.T) {
	for _, c := range []struct {
		in, cur string
		want    int64
		err     string
	}{
		{"30", "CHF", 3000, ""},
		{" 30 ", "CHF", 3000, ""},
		{"29.5", "CHF", 2950, ""},
		{"29.50", "CHF", 2950, ""},
		{"0.01", "CHF", 1, ""},
		{"3000", "JPY", 3000, ""},
		{"29,50", "CHF", 0, "dot"},
		{"1,000", "CHF", 0, "dot"},
		{"29.505", "CHF", 0, "two decimal"},
		{"30.", "CHF", 0, "such as"},
		{"3e3", "CHF", 0, "such as"},
		{"-5", "CHF", 0, "such as"},
		{"0", "CHF", 0, "greater than 0"},
		{"30.5", "JPY", 0, "whole amount"},
		{"30", "XXX", 0, "unknown currency"},
		{"99999999999", "CHF", 0, "too large"},
	} {
		got, err := ParseMajor(c.in, c.cur)
		if c.err == "" {
			if err != nil || got != c.want {
				t.Errorf("%q %s: got %d %v, want %d", c.in, c.cur, got, err, c.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%q %s: want an error containing %q, got %d %v", c.in, c.cur, c.err, got, err)
		}
	}
}

func TestFormatMinor(t *testing.T) {
	for _, c := range []struct {
		minor int64
		cur   string
		want  string
	}{
		{3000, "CHF", "30.00 CHF"},
		{5, "CHF", "0.05 CHF"},
		{30050, "EUR", "300.50 EUR"},
		{5000, "JPY", "5000 JPY"},
	} {
		if got := FormatMinor(c.minor, c.cur); got != c.want {
			t.Errorf("%d %s: got %q, want %q", c.minor, c.cur, got, c.want)
		}
	}
}

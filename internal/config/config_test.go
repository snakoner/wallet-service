package config

import (
	"strings"
	"testing"
)

func TestKEKs(t *testing.T) {
	kek1, kek2, kek3 := strings.Repeat("01", 32), strings.Repeat("02", 32), strings.Repeat("03", 32)

	keks, err := KeyStorageConfig{KEK: kek3, KEKVersion: 3, OldKEKs: "1:" + kek1 + ", 2:" + kek2}.KEKs()
	if err != nil || len(keks) != 3 || keks[1][0] != 1 || keks[2][0] != 2 || keks[3][0] != 3 {
		t.Fatalf("keks = %v, err = %v", keks, err)
	}
	if keks, err := (KeyStorageConfig{KEK: kek1, KEKVersion: 1}).KEKs(); err != nil || len(keks) != 1 {
		t.Fatalf("without old keks: keks = %v, err = %v", keks, err)
	}

	for name, cfg := range map[string]KeyStorageConfig{
		"bad current":         {KEK: "zz", KEKVersion: 1},
		"short current":       {KEK: "0102", KEKVersion: 1},
		"zero version":        {KEK: kek1, KEKVersion: 0},
		"old same as current": {KEK: kek1, KEKVersion: 1, OldKEKs: "1:" + kek2},
		"old duplicate":       {KEK: kek3, KEKVersion: 3, OldKEKs: "1:" + kek1 + ",1:" + kek2},
		"old without version": {KEK: kek3, KEKVersion: 3, OldKEKs: kek1},
		"old bad version":     {KEK: kek3, KEKVersion: 3, OldKEKs: "v1:" + kek1},
		"old bad hex":         {KEK: kek3, KEKVersion: 3, OldKEKs: "1:zz"},
		"old negative":        {KEK: kek3, KEKVersion: 3, OldKEKs: "-1:" + kek1},
	} {
		if _, err := cfg.KEKs(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

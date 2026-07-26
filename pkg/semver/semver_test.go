package semver

import "testing"

func TestValid(t *testing.T) {
	for _, v := range []string{"1.0.0", "0.6.19", "v2.3.4", "1.2"} {
		if !Valid(v) {
			t.Errorf("Valid(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "abc", "1.x", "latest"} {
		if Valid(v) {
			t.Errorf("Valid(%q) = true, want false", v)
		}
	}
}

func TestValidConstraint(t *testing.T) {
	for _, c := range []string{">=1.2.0", "^1.0", "~1.2", ">=1.2 <2.0", ">=1 || >=3"} {
		if !ValidConstraint(c) {
			t.Errorf("ValidConstraint(%q) = false, want true", c)
		}
	}
	if ValidConstraint("not a constraint!!") {
		t.Error("garbage constraint accepted")
	}
}

func TestSatisfies(t *testing.T) {
	cases := []struct {
		version, constraint string
		want                bool
	}{
		{"1.2.0", ">=1.0.0", true},
		{"0.9.0", ">=1.0.0", false},
		{"1.5.0", "^1.0.0", true},
		{"2.0.0", "^1.0.0", false},
		{"1.2.3", ">=1.2 <2.0", true},
		{"2.1.0", ">=1.2 <2.0", false},
	}
	for _, tc := range cases {
		got, err := Satisfies(tc.version, tc.constraint)
		if err != nil {
			t.Errorf("Satisfies(%q,%q) error: %v", tc.version, tc.constraint, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Satisfies(%q,%q) = %v, want %v", tc.version, tc.constraint, got, tc.want)
		}
	}
	if _, err := Satisfies("bad", ">=1.0.0"); err == nil {
		t.Error("expected error for invalid version")
	}
	if _, err := Satisfies("1.0.0", "@@@"); err == nil {
		t.Error("expected error for invalid constraint")
	}
}

func TestCompare(t *testing.T) {
	if c, _ := Compare("1.0.0", "2.0.0"); c != -1 {
		t.Errorf("Compare(1.0.0,2.0.0) = %d, want -1", c)
	}
	if c, _ := Compare("2.0.0", "2.0.0"); c != 0 {
		t.Errorf("Compare equal = %d, want 0", c)
	}
	if c, _ := Compare("2.1.0", "2.0.0"); c != 1 {
		t.Errorf("Compare greater = %d, want 1", c)
	}
}

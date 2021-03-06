package main

import (
	"testing"
)

func TestBasicAuthParsing(t *testing.T) {
	tests := []struct {
		input, user, pass string
		ok                bool
	}{
		{"Notbasic QWxhZGRpbjpvcGVuIHNlc2FtZQ==", "", "", false},
		{"Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==", "Aladdin", "open sesame", true},
		{"Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ=x", "", "", false},
	}

	for _, test := range tests {
		gu, gp, err := parseBasicAuth(test.input)
		if test.ok {
			if err != nil {
				t.Errorf("Expected no error on %v, got %v",
					test.input, err)
			}
			if gu != test.user {
				t.Errorf("Expected user=%v for %v, got %v",
					test.user, test.input, gu)
			}
			if gp != test.pass {
				t.Errorf("Expected pass=%v for %v, got %v",
					test.pass, test.input, gp)
			}
		} else {
			if err == nil {
				t.Errorf("Expected error on %v, got %v/%v",
					test.input, gu, gp)
			}
		}
	}
}

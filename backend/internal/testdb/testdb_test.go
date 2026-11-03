package testdb

import "testing"

func TestValidateURL(t *testing.T) {
	for _, raw := range []string{
		"postgres://user:secret@production.example/agroshield_test",
		"postgres://user:secret@localhost/production",
		"postgres://user:secret@localhost/agroshield_test?host=production.example",
		"postgres://user:secret@localhost/agroshield_test?service=production",
		"postgres://user:secret@localhost/agroshield_test?search_path=public",
		"invalid", "postgres://%gh",
	} {
		if _, err := ValidateURL(raw); err == nil {
			t.Error("unsafe URL accepted")
		}
	}
	if _, err := ValidateURL("postgres://agro_test@127.0.0.1:55438/agroshield_test?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
}

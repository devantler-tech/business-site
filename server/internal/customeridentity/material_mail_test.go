package customeridentity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// materialMailSettings applies test overrides to the existing phase settings.
func materialMailSettings(t *testing.T, input string, changes map[string]string) {
	t.Helper()
	var settings map[string]string
	if err := json.Unmarshal(materialFixtureSettings(t, input), &settings); err != nil {
		t.Fatal(err)
	}
	for key, value := range changes {
		settings[key] = value
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	materialWrite(t, input, "settings.json", data)
}

// materialMailInputs adds coherent fictional SMTP inputs and returns the password for output checks.
func materialMailInputs(t *testing.T, input string) []byte {
	t.Helper()
	materialMailSettings(t, input, map[string]string{
		"smtpHost": "submission.example.invalid",
		"smtpPort": "465",
		"smtpFrom": "invitations@example.invalid",
		"smtpUser": "fictional-sender",
	})
	password := materialPassword(t)
	materialWrite(t, input, "smtp-password", password)
	return password
}

// TestMaterializeSetupMail checks the native SMTP document and the scope of its password.
func TestMaterializeSetupMail(t *testing.T) {
	input, output := materialFixture(t, "setup")
	password := materialMailInputs(t, input)
	if err := Materialize("setup-mail", input, output); err != nil {
		t.Fatalf("explicit setup profile refused coherent mail inputs: %v", err)
	}
	materialAssertOutput(t, output, "setup-mail")
	var config struct {
		DefaultInstance struct {
			SMTPConfiguration struct {
				TLS  bool `json:"Tls"`
				From string
				SMTP struct {
					Host      string
					PlainAuth struct{ User, Password string }
				}
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(output, "config.json"))
	if err != nil || json.Unmarshal(data, &config) != nil {
		t.Fatal("native setup configuration missing or malformed")
	}
	mail := config.DefaultInstance.SMTPConfiguration
	if !mail.TLS || mail.From != "invitations@example.invalid" ||
		mail.SMTP.Host != "submission.example.invalid:465" ||
		mail.SMTP.PlainAuth.User != "fictional-sender" ||
		mail.SMTP.PlainAuth.Password != string(password) {
		t.Fatal("native setup mail configuration differs from selected inputs")
	}
	for _, name := range []string{"steps.json", "ca.pem", "master.key", "tls.crt", "tls.key"} {
		data, err := os.ReadFile(filepath.Join(output, name))
		if err != nil || bytes.Contains(data, password) {
			t.Fatalf("mail credential crossed its private setup configuration boundary: %s", name)
		}
	}
}

// TestMaterializeSetupMailRefusals checks that invalid mail inputs leave no output.
func TestMaterializeSetupMailRefusals(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing password": func(t *testing.T, input string) {
			if err := os.Remove(filepath.Join(input, "smtp-password")); err != nil {
				t.Fatal(err)
			}
		},
		"unresolved password": func(t *testing.T, input string) {
			materialWrite(t, input, "smtp-password", []byte("${CUSTOMER_SMTP_PASSWORD}"))
		},
		"reused database password": func(t *testing.T, input string) {
			data, err := os.ReadFile(filepath.Join(input, "database-password"))
			if err != nil {
				t.Fatal(err)
			}
			materialWrite(t, input, "smtp-password", data)
		},
		"reused bootstrap password": func(t *testing.T, input string) {
			data, err := os.ReadFile(filepath.Join(input, "bootstrap-password"))
			if err != nil {
				t.Fatal(err)
			}
			materialWrite(t, input, "smtp-password", data)
		},
		"reused master key": func(t *testing.T, input string) {
			data := materialPassword(t)[:32]
			materialWrite(t, input, "master.key", data)
			materialWrite(t, input, "smtp-password", data)
		},
		"invalid host": func(t *testing.T, input string) {
			materialMailSettings(t, input, map[string]string{"smtpHost": "submission.example.invalid:465"})
		},
		"unexpected port": func(t *testing.T, input string) {
			materialMailSettings(t, input, map[string]string{"smtpPort": "25"})
		},
		"ambiguous sender": func(t *testing.T, input string) {
			materialMailSettings(t, input, map[string]string{"smtpFrom": "Display <invitations@example.invalid>"})
		},
		"sender address literal": func(t *testing.T, input string) {
			materialMailSettings(t, input, map[string]string{"smtpFrom": "invitations@[192.0.2.1]"})
		},
		"non-ASCII sender": func(t *testing.T, input string) {
			materialMailSettings(t, input, map[string]string{"smtpFrom": "invitat\u00e9@example.invalid"})
		},
		"non-ASCII credential user": func(t *testing.T, input string) {
			materialMailSettings(t, input, map[string]string{"smtpUser": "us\u00e9r"})
		},
		"credential user control character": func(t *testing.T, input string) {
			materialMailSettings(t, input, map[string]string{"smtpUser": "fictional\tuser"})
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input, output := materialFixture(t, "setup")
			materialMailInputs(t, input)
			mutate(t, input)
			if err := Materialize("setup-mail", input, output); err == nil {
				t.Fatal("invalid mail inputs accepted")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("invalid mail inputs reserved output")
			}
		})
	}
	for _, phase := range []string{"init", "setup", "serve"} {
		t.Run(phase, func(t *testing.T) {
			input, output := materialFixture(t, phase)
			materialMailInputs(t, input)
			if err := Materialize(phase, input, output); err == nil {
				t.Fatal("ordinary phase accepted mail inputs")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("mail inputs reserved output outside explicit setup")
			}
		})
	}
}

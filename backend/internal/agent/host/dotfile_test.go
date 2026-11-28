package host

import "testing"

func TestDotfileEligibilityAllowsSafeConfigAndBlocksSecrets(t *testing.T) {
	allowed := []string{
		".gitignore",
		".github/workflows/ci.yml",
		".prettierrc",
		".prettierrc.json",
		".eslintrc.json",
		".eslintignore",
		".node-version",
		".python-version",
		".golangci.yml",
		".dockerignore",
		"frontend/.prettierrc.json",
	}
	for _, p := range allowed {
		if !eligible(p) {
			t.Errorf("expected safe project dotfile %q to be eligible", p)
		}
	}
	blocked := []string{
		".git/config",
		".env",
		".env.local",
		".npmrc",
		".pypirc",
		".netrc",
		".local/config.yaml",
		".codex/state.json",
		"secrets.json",
		"credentials.json",
		"certs/server.pem",
	}
	for _, p := range blocked {
		if eligible(p) {
			t.Errorf("expected sensitive dotfile %q to be blocked", p)
		}
	}
}

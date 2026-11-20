package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	apperrors "github.com/noosphere/backend/internal/errors"
	"github.com/noosphere/backend/internal/gateway/secure"
)

// Unprefixed values from Windows releases are legacy AES-GCM ciphertext.
// Only an authenticated decrypt can promote a value into an API key.
const credentialPrefix = "enc:v1:"

type persistedModelConfig struct {
	ModelConfig     `yaml:",inline"`
	LegacyAPIKey    string `yaml:"api_key,omitempty"`
	EncryptedAPIKey string `yaml:"encrypted_api_key,omitempty"`
}

type persistedGatewayConfig struct {
	DefaultModel string                 `yaml:"default_model"`
	Models       []persistedModelConfig `yaml:"providers"`
}

func (m persistedModelConfig) savedCredential() string {
	// Older clients would forward undecipherable api_key values. Only read
	// that field for migration and keep it empty on every new write.
	if m.EncryptedAPIKey != "" {
		return m.EncryptedAPIKey
	}
	return m.LegacyAPIKey
}

type CredentialStatus struct {
	Provider string        `json:"provider"`
	Status   secure.Status `json:"status"`
	Reason   string        `json:"reason"`
}

func credentialCode(status secure.Status) apperrors.Code {
	return apperrors.Code("CREDENTIAL_" + strings.ToUpper(string(status)))
}

func credentialReason(status secure.Status) string {
	switch status {
	case secure.NotFound:
		return "找不到保存此连接时使用的系统密钥，请重新授权。"
	case secure.Locked:
		return "系统凭证库已锁定，请解锁后重载配置，或重新输入连接凭证。"
	case secure.PermissionDenied:
		return "系统凭证库拒绝访问，请检查凭证库权限。"
	case secure.Corrupt:
		return "保存的凭证无法验证，请重新授权。"
	default:
		return "系统凭证库不可用。无需凭证的本地模型仍可使用。"
	}
}

func (m *ModelConfig) credentialError() *apperrors.AppError {
	if m.Credential == nil {
		return nil
	}
	failure := apperrors.New(credentialCode(m.Credential.Status), apperrors.CatGateway, m.Credential.Reason)
	failure.HTTPStatus = http.StatusServiceUnavailable
	return failure
}

func (g *Gateway) loadCredentialKey() ([]byte, *secure.StoreError) {
	if g.secretStore == nil {
		if len(g.encKey) == 32 {
			return append([]byte(nil), g.encKey...), nil
		}
		return nil, &secure.StoreError{Provider: "unavailable", Status: secure.Unavailable}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key, err := secure.LoadKey(ctx, g.secretStore)
	if err == nil {
		return key, nil
	}
	return nil, credentialStoreError(g.secretStore, err)
}

func (g *Gateway) keyForNewCredential() ([]byte, error) {
	if g.secretStore == nil {
		if len(g.encKey) == 32 {
			return append([]byte(nil), g.encKey...), nil
		}
		return nil, &secure.StoreError{Provider: "unavailable", Status: secure.Unavailable}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key, err := secure.GetOrCreateKey(ctx, g.secretStore)
	if err != nil {
		return nil, credentialStoreError(g.secretStore, err)
	}
	return key, nil
}

func credentialStoreError(store secure.SecretStore, err error) *secure.StoreError {
	var failure *secure.StoreError
	if errors.As(err, &failure) {
		return failure
	}
	return &secure.StoreError{Provider: store.Provider(), Status: secure.Unavailable}
}

func (m *ModelConfig) decodeCredential(saved string, key []byte, provider string, failure *secure.StoreError) {
	if saved == "" {
		return
	}
	m.storedCredential = saved
	if failure == nil {
		encoded := strings.TrimPrefix(m.storedCredential, credentialPrefix)
		plaintext, err := secure.Decrypt(key, encoded)
		if err == nil && plaintext != "" {
			m.APIKey = plaintext
			m.storedCredential = credentialPrefix + encoded
			return
		}
		failure = &secure.StoreError{Provider: provider, Status: secure.Corrupt}
	}
	m.Credential = &CredentialStatus{Provider: failure.Provider, Status: failure.Status, Reason: credentialReason(failure.Status)}
}

func modelSaveError(err error) *apperrors.AppError {
	var failure *secure.StoreError
	if errors.As(err, &failure) {
		result := apperrors.New(credentialCode(failure.Status), apperrors.CatGateway, credentialReason(failure.Status))
		result.HTTPStatus = http.StatusServiceUnavailable
		return result
	}
	return apperrors.Wrap(err, apperrors.ErrInternal, apperrors.CatGateway, "failed to save model configuration")
}

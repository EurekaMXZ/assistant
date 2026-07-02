package credential

import (
	"context"
	"errors"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

type StoredCredentialReader interface {
	GetStored(ctx context.Context, id string) (*domain.StoredProviderCredential, error)
}

type Resolved struct {
	Provider string
	BaseURL  string
	APIKey   string
}

type Resolver struct {
	store  StoredCredentialReader
	cipher *Cipher
}

func NewResolver(store StoredCredentialReader, cipher *Cipher) *Resolver {
	return &Resolver{store: store, cipher: cipher}
}

func (r *Resolver) ResolveCredential(ctx context.Context, credentialID string) (*Resolved, error) {
	if r == nil || r.store == nil || r.cipher == nil {
		return nil, errors.New("provider credential resolver is not configured")
	}
	stored, err := r.store.GetStored(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	if stored.Status != domain.CredentialStatusEnabled {
		return nil, errors.New("provider credential is unavailable")
	}
	apiKey, err := r.cipher.Decrypt(stored.ID, stored.Provider, stored.EncryptedAPIKey, stored.Nonce)
	if err != nil {
		return nil, err
	}
	return &Resolved{Provider: stored.Provider, BaseURL: stored.BaseURL, APIKey: apiKey}, nil
}

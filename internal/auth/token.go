package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/golang-jwt/jwt/v5"
)

const defaultTokenIssuer = "assistant"

type TokenSettings struct {
	Secret         string
	Issuer         string
	AccessTokenTTL time.Duration
}

type TokenService struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

type AccessTokenClaims struct {
	jwt.RegisteredClaims
	AuthVersion int64 `json:"auth_version"`
}

func NewTokenService(settings TokenSettings) (*TokenService, error) {
	secret := strings.TrimSpace(settings.Secret)
	if secret == "" {
		return nil, errors.New("token service requires secret")
	}

	issuer := strings.TrimSpace(settings.Issuer)
	if issuer == "" {
		issuer = defaultTokenIssuer
	}

	ttl := settings.AccessTokenTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return &TokenService{
		secret: []byte(secret),
		issuer: issuer,
		ttl:    ttl,
		now:    time.Now,
	}, nil
}

func (s *TokenService) Issue(user *domain.User) (string, time.Time, error) {
	if s == nil {
		return "", time.Time{}, errors.New("token service is nil")
	}
	if user == nil || strings.TrimSpace(user.ID) == "" {
		return "", time.Time{}, errors.New("token service requires user id")
	}

	issuedAt := s.now().UTC()
	expiresAt := issuedAt.Add(s.ttl)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, AccessTokenClaims{
		AuthVersion: user.AuthVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})

	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}

	return signed, expiresAt, nil
}

func (s *TokenService) Parse(token string) (*AccessTokenClaims, error) {
	if s == nil {
		return nil, errors.New("token service is nil")
	}

	claims := &AccessTokenClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(parsed *jwt.Token) (any, error) {
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, domain.NewUnauthorizedError("invalid access token")
	}
	if !parsed.Valid {
		return nil, domain.NewUnauthorizedError("invalid access token")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return nil, domain.NewUnauthorizedError("invalid access token")
	}

	return claims, nil
}

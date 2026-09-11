package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenKind различает access и refresh токены.
type TokenKind string

const (
	TokenAccess  TokenKind = "access"
	TokenRefresh TokenKind = "refresh"
)

// Claims — полезные данные JWT.
type Claims struct {
	UserID   string    `json:"sub"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	Kind     TokenKind `json:"kind"`
	jwt.RegisteredClaims
}

// TokenService выпускает и проверяет JWT.
type TokenService struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewTokenService создает сервис токенов.
func NewTokenService(secret string, accessTTL, refreshTTL time.Duration) *TokenService {
	return &TokenService{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// Issue выпускает пару токенов access/refresh.
func (s *TokenService) Issue(userID uuid.UUID, username, role string) (string, string, error) {
	access, err := s.build(userID, username, role, TokenAccess, s.accessTTL)
	if err != nil {
		return "", "", err
	}

	refresh, err := s.build(userID, username, role, TokenRefresh, s.refreshTTL)
	if err != nil {
		return "", "", err
	}

	return access, refresh, nil
}

func (s *TokenService) build(userID uuid.UUID, username, role string, kind TokenKind, ttl time.Duration) (string, error) {
	now := time.Now()

	claims := Claims{
		UserID:   userID.String(),
		Username: username,
		Role:     role,
		Kind:     kind,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "video-surveillance",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// Parse проверяет подпись, срок действия и тип токена.
func (s *TokenService) Parse(tokenString string, kind TokenKind) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	if claims.Kind != kind {
		return nil, fmt.Errorf("unexpected token kind: %s", claims.Kind)
	}

	return claims, nil
}
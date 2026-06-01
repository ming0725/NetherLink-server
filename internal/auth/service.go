package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"nlserver/internal/config"
	"nlserver/internal/repository"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrValidation         = errors.New("auth: validation failed")
	ErrConflict           = errors.New("auth: conflict")
	ErrInactiveUser       = errors.New("auth: inactive user")
)

var publicIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]{4,32}$`)

type Service struct {
	cfg   config.AuthConfig
	repos repository.Repositories
	now   func() time.Time
}

type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
	PublicID    string
	Gender      string
	DeviceID    string
	DeviceName  string
}

type LoginInput struct {
	Account    string
	Password   string
	DeviceID   string
	DeviceName string
}

type RefreshInput struct {
	RefreshToken string
	DeviceID     string
}

type AuthResult struct {
	User         repository.User
	Preferences  repository.UserPreference
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

type Principal struct {
	UserID       string
	SessionID    string
	DeviceID     string
	TokenVersion int
}

func NewService(cfg config.AuthConfig, repos repository.Repositories) *Service {
	if cfg.TokenSecret == "" {
		cfg.TokenSecret = "nlserver-local-dev-token-secret-change-me"
	}
	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = 15 * time.Minute
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = 30 * 24 * time.Hour
	}

	return &Service{
		cfg:   cfg,
		repos: repos,
		now:   time.Now,
	}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (AuthResult, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return AuthResult{}, err
	}
	if err := validatePassword(input.Password); err != nil {
		return AuthResult{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return AuthResult{}, ErrValidation
	}

	publicID := strings.TrimSpace(input.PublicID)
	if publicID == "" {
		publicID, err = s.generateAvailablePublicID(ctx)
		if err != nil {
			return AuthResult{}, err
		}
	} else if !validPublicID(publicID) {
		return AuthResult{}, ErrValidation
	}

	gender := strings.TrimSpace(input.Gender)
	if gender == "" {
		gender = "unspecified"
	}

	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		return AuthResult{}, err
	}

	user, err := s.repos.Users.Create(ctx, repository.CreateUserParams{
		PublicID:     publicID,
		Email:        email,
		PasswordHash: passwordHash,
		Nick:         displayName,
		Gender:       gender,
		Status:       "active",
	})
	if err != nil {
		return AuthResult{}, mapRepositoryError(err)
	}

	preferences, err := s.repos.Preferences.EnsureDefault(ctx, user.ID)
	if err != nil {
		return AuthResult{}, err
	}

	result, err := s.createTokenSession(ctx, user, deviceIDOrDefault(input.DeviceID), stringPtrOrNil(input.DeviceName))
	if err != nil {
		return AuthResult{}, err
	}
	result.Preferences = preferences
	return result, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput) (AuthResult, error) {
	if strings.TrimSpace(input.Account) == "" || strings.TrimSpace(input.Password) == "" || strings.TrimSpace(input.DeviceID) == "" {
		return AuthResult{}, ErrValidation
	}

	user, err := s.repos.Users.GetByAccount(ctx, input.Account)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return AuthResult{}, ErrInvalidCredentials
		}
		return AuthResult{}, err
	}
	if user.Status != "active" {
		return AuthResult{}, ErrInvalidCredentials
	}
	if !verifyPassword(input.Password, user.PasswordHash) {
		return AuthResult{}, ErrInvalidCredentials
	}

	preferences, err := s.repos.Preferences.EnsureDefault(ctx, user.ID)
	if err != nil {
		return AuthResult{}, err
	}

	result, err := s.createTokenSession(ctx, user, strings.TrimSpace(input.DeviceID), stringPtrOrNil(input.DeviceName))
	if err != nil {
		return AuthResult{}, err
	}
	result.Preferences = preferences

	_ = s.repos.Users.TouchLastSeen(ctx, user.ID, s.now().UTC())
	return result, nil
}

func (s *Service) Refresh(ctx context.Context, input RefreshInput) (RefreshResult, error) {
	if strings.TrimSpace(input.RefreshToken) == "" || strings.TrimSpace(input.DeviceID) == "" {
		return RefreshResult{}, ErrTokenInvalid
	}

	oldHash := hashRefreshToken(strings.TrimSpace(input.RefreshToken))
	session, err := s.repos.Sessions.GetActiveByRefreshTokenHash(ctx, oldHash)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return RefreshResult{}, ErrTokenInvalid
		}
		return RefreshResult{}, err
	}
	if session.DeviceID != strings.TrimSpace(input.DeviceID) {
		return RefreshResult{}, ErrTokenInvalid
	}

	user, err := s.repos.Users.GetByID(ctx, session.UserID)
	if err != nil {
		return RefreshResult{}, mapRepositoryError(err)
	}
	if user.Status != "active" {
		return RefreshResult{}, ErrTokenInvalid
	}

	refreshToken, err := newRefreshToken()
	if err != nil {
		return RefreshResult{}, err
	}

	_, err = s.repos.Sessions.RotateRefreshToken(
		ctx,
		session.ID,
		oldHash,
		hashRefreshToken(refreshToken),
		s.now().UTC().Add(s.cfg.RefreshTokenTTL),
	)
	if err != nil {
		return RefreshResult{}, mapRepositoryError(err)
	}

	accessToken, err := s.mintAccessToken(user, session.ID, session.DeviceID)
	if err != nil {
		return RefreshResult{}, err
	}

	return RefreshResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresInSeconds(s.cfg.AccessTokenTTL),
	}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	token := strings.TrimSpace(refreshToken)
	if token == "" {
		return ErrTokenInvalid
	}

	err := s.repos.Sessions.RevokeByRefreshTokenHash(ctx, hashRefreshToken(token))
	if err != nil {
		return mapRepositoryError(err)
	}
	return nil
}

func (s *Service) ValidateAccessToken(ctx context.Context, token string) (Principal, error) {
	claims, err := verifyAccessToken(s.cfg.TokenSecret, token, s.now().UTC())
	if err != nil {
		return Principal{}, err
	}

	session, err := s.repos.Sessions.GetActiveByID(ctx, claims.SessionID)
	if err != nil {
		return Principal{}, mapRepositoryError(err)
	}
	if session.UserID != claims.Subject || session.DeviceID != claims.DeviceID {
		return Principal{}, ErrTokenInvalid
	}

	user, err := s.repos.Users.GetByID(ctx, claims.Subject)
	if err != nil {
		return Principal{}, mapRepositoryError(err)
	}
	if user.Status != "active" || user.TokenVersion != claims.TokenVersion {
		return Principal{}, ErrTokenInvalid
	}

	return Principal{
		UserID:       claims.Subject,
		SessionID:    claims.SessionID,
		DeviceID:     claims.DeviceID,
		TokenVersion: claims.TokenVersion,
	}, nil
}

func (s *Service) createTokenSession(ctx context.Context, user repository.User, deviceID string, deviceName *string) (AuthResult, error) {
	refreshToken, err := newRefreshToken()
	if err != nil {
		return AuthResult{}, err
	}

	now := s.now().UTC()
	session, err := s.repos.Sessions.Create(ctx, repository.CreateUserSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: hashRefreshToken(refreshToken),
		DeviceID:         deviceID,
		DeviceName:       deviceName,
		ExpiresAt:        now.Add(s.cfg.RefreshTokenTTL),
		LastSeenAt:       &now,
	})
	if err != nil {
		return AuthResult{}, mapRepositoryError(err)
	}

	accessToken, err := s.mintAccessToken(user, session.ID, session.DeviceID)
	if err != nil {
		return AuthResult{}, err
	}

	return AuthResult{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    expiresInSeconds(s.cfg.AccessTokenTTL),
	}, nil
}

func (s *Service) mintAccessToken(user repository.User, sessionID string, deviceID string) (string, error) {
	now := s.now().UTC()
	return signAccessToken(s.cfg.TokenSecret, AccessClaims{
		Type:         tokenTypeAccess,
		Subject:      user.ID,
		SessionID:    sessionID,
		DeviceID:     deviceID,
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(s.cfg.AccessTokenTTL).Unix(),
		Scope:        tokenScopeUser,
		TokenVersion: user.TokenVersion,
	})
}

func (s *Service) generateAvailablePublicID(ctx context.Context) (string, error) {
	for i := 0; i < 8; i++ {
		publicID, err := randomPublicID()
		if err != nil {
			return "", err
		}

		exists, err := s.repos.Users.PublicIDExists(ctx, publicID)
		if err != nil {
			return "", err
		}
		if !exists {
			return publicID, nil
		}
	}

	return "", ErrConflict
}

func randomPublicID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}

	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random))
	return "nl_" + encoded[:10], nil
}

func normalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" {
		return "", ErrValidation
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", ErrValidation
	}
	return email, nil
}

func validPublicID(value string) bool {
	return publicIDPattern.MatchString(strings.TrimSpace(value))
}

func deviceIDOrDefault(value string) string {
	deviceID := strings.TrimSpace(value)
	if deviceID == "" {
		return "default"
	}
	return deviceID
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func mapRepositoryError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ErrTokenInvalid
	case errors.Is(err, repository.ErrConflict):
		return ErrConflict
	default:
		return err
	}
}
